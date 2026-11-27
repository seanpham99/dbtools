package dump

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"sort"
	"strings"
)

// MSSQL's auto-generated constraint names embed a random per-database hex
// suffix: SQL Server derives the name when a migration leaves a PRIMARY KEY,
// UNIQUE, FOREIGN KEY, CHECK, or DEFAULT constraint unnamed, producing shapes
// like PK__fiinprox__3213E83F732A0327 or DF__t_df__y__46E78A0C. Two scratch
// databases replaying identical migrations therefore hold different names,
// and a schema dump committed as a squash baseline differs byte-for-byte
// between runs — which breaks the ledger's content hash, since it is taken
// over the emitted bytes.
//
// NormalizeMSSQLConstraintNames rewrites every auto-generated name in dump
// output to a deterministic form: the random hex tail is replaced by the
// FNV-1a hash of the name's stable prefix (everything before the hex tail).
// The prefix already encodes the constraint kind and the truncated
// table/column identity SQL Server chose, so the result keeps the
// PREFIX__object__HEX shape while becoming stable across databases.
//
// Every occurrence of a renamed identifier is rewritten — CONSTRAINT
// clauses, ADD CONSTRAINT, CREATE INDEX guards, and the IF NOT EXISTS
// probes that quote the same name — so the script stays internally
// consistent. Constraint names are database-scoped in sys.*, so one name
// refers to exactly one object and a quoted-name replace cannot hit a
// second constraint. Explicitly named constraints are untouched unless
// they happen to match the auto-name shape exactly (a user naming a
// constraint PK__foo__DEADBEEF gets it canonicalized too — harmless,
// since the canonical form is equally valid and stable).
//
// Residual risk, accepted: a name mentioned inside a module body emitted
// verbatim from sys.sql_modules (EXEC(N'...')-wrapped CREATE FUNCTION,
// CREATE OR ALTER PROC/VIEW) is also rewritten if it appears in a quoted
// form matching an auto-name. Module bodies containing constraint DDL
// that names an auto-generated constraint are vanishingly rare, and the
// rewrite keeps the reference pointing at the renamed object rather than
// breaking it.
var (
	// autoConstraintName matches SQL Server's unnamed-constraint shape:
	// a two-letter kind prefix, two underscores, a (possibly itself
	// underscored, truncated) object path, two underscores, then a hex
	// tail — 16 chars for index-backed constraints (PK/UQ/FK), 8 for
	// column-scoped ones (CK/DF). Group 1 is the stable prefix kept in
	// the canonical name; group 2 is the random tail replaced by a hash.
	autoConstraintName = regexp.MustCompile(`(?i)^((?:PK|UQ|FK|CK|DF)__.+?__)(?:[0-9A-F]{16}|[0-9A-F]{8})$`)

	// Quoted occurrences of a name in dump output appear either bracketed
	// ([name]) or as a string literal (N'name' / 'name'). Both forms are
	// rewritten so DDL declarations and catalog probes stay in sync. The
	// bracket form admits any non-']' byte because auto-generated names
	// inherit whatever characters the truncated object name carried
	// (spaces included); the auto-name pattern still decides what is a
	// rename candidate.
	quotedNameBrackets = regexp.MustCompile(`\[([^\]\r\n]+)\]`)
	quotedNameLiteral  = regexp.MustCompile(`(?i)(N?)'([^'\r\n]+)'`)
)

// NormalizeMSSQLConstraintNames canonicalizes auto-generated constraint
// names in MSSQL dump output. See the package comment above for the
// naming scheme and residual risks.
func NormalizeMSSQLConstraintNames(sqlText string) string {
	// Collect every distinct auto-generated name first: the canonical
	// target is a function of the stable prefix, and collisions between
	// two different random tails sharing a prefix are resolved by
	// appending a deterministic _N suffix in sorted order.
	autoNames := make(map[string]bool)
	for _, m := range quotedNameBrackets.FindAllStringSubmatch(sqlText, -1) {
		if autoConstraintName.MatchString(m[1]) {
			autoNames[m[1]] = true
		}
	}
	for _, m := range quotedNameLiteral.FindAllStringSubmatch(sqlText, -1) {
		if autoConstraintName.MatchString(m[2]) {
			autoNames[m[2]] = true
		}
	}
	if len(autoNames) == 0 {
		return sqlText
	}

	names := make([]string, 0, len(autoNames))
	for n := range autoNames {
		names = append(names, n)
	}
	sort.Strings(names)

	renames := make(map[string]string, len(names))
	used := make(map[string]bool, len(names))
	for _, old := range names {
		prefix := autoConstraintName.FindStringSubmatch(old)[1]
		base := prefix + canonicalHex(prefix)
		candidate := base
		for i := 2; used[candidate]; i++ {
			candidate = fmt.Sprintf("%s_%d", base, i)
		}
		used[candidate] = true
		renames[old] = candidate
	}

	sqlText = quotedNameBrackets.ReplaceAllStringFunc(sqlText, func(tok string) string {
		name := tok[1 : len(tok)-1]
		if newName, ok := renames[name]; ok {
			return "[" + newName + "]"
		}
		return tok
	})
	return quotedNameLiteral.ReplaceAllStringFunc(sqlText, func(tok string) string {
		m := quotedNameLiteral.FindStringSubmatch(tok)
		if newName, ok := renames[m[2]]; ok {
			return m[1] + "'" + newName + "'"
		}
		return tok
	})
}

// canonicalHex derives the deterministic 8-hex tail from the stable
// prefix, mirroring SQL Server's own hex-suffix shape.
func canonicalHex(prefix string) string {
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(prefix)))
	return fmt.Sprintf("%08X", h.Sum32())
}
