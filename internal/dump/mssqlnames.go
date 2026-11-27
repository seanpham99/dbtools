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
// Collision handling: two constraints can share a truncated prefix
// (PK__shared__) while carrying different random tails. Canonical names
// are assigned in first-occurrence order in the dump text — emission
// order is fixed by the dump query (sys.objects/object_id ordering), so
// it is stable across scratch databases even though the random tails
// themselves are not. Sorting by byte position rather than by the random
// tail means two databases that exchange tails on the same objects still
// assign the same canonical name to the same position. Before suffix
// allocation, every explicitly named (non-auto) identifier quoted in the
// dump is reserved so a canonical candidate can never collide with a
// name that stays unchanged.
//
// Quoted names are matched after decoding SQL Server's escape sequences
// (]] inside bracketed identifiers, ” inside string literals) and
// re-escaped on write-back, so objects whose names contain ] or ' round-
// trip correctly.
//
// Literal-form occurrences (N'...') are rewritten only inside catalog
// probes — the name = N'...' predicate of sys.* guard queries — and
// CONSTRAINT clauses. A quoted literal that merely resembles an auto
// name, e.g. a DEFAULT constant DEFAULT (N'PK__sentinel__12345678'), is
// data, not an identifier, and is left alone.
//
// Residual risk, accepted: a name mentioned inside a module body emitted
// verbatim from sys.sql_modules (EXEC(N'...')-wrapped CREATE FUNCTION,
// CREATE OR ALTER PROC/VIEW) is also rewritten if it appears in a bracketed
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
	// bracket content admits ]] escape pairs so identifiers containing
	// ] parse to their end; the literal content admits '' escape pairs
	// for the same reason. The auto-name pattern still decides what is a
	// rename candidate.
	quotedNameBrackets = regexp.MustCompile(`\[((?:[^\]\r\n]|\]\])*)\]`)
	quotedNameLiteral  = regexp.MustCompile(`(?i)(N?)'((?:[^'\r\n]|'')*)'`)

	// Literal-form occurrences are rewritten only where the literal is
	// really a constraint identifier: the name = N'...' predicate of a
	// sys.* catalog probe, or a CONSTRAINT clause. literalNameEquals and
	// literalConstraintTail match the tail of the text immediately
	// preceding a literal; literalCatalogProbe must appear earlier in
	// the same GO batch for the name = form, so a data literal such as
	// CHECK ([name] = N'PK__sentinel__12345678') or a DEFAULT constant
	// is never touched.
	literalNameEquals     = regexp.MustCompile(`(?i)\[?\bname\]?\s*=\s*$`)
	literalConstraintTail = regexp.MustCompile(`(?i)\bCONSTRAINT\s+$`)
	literalCatalogProbe   = regexp.MustCompile(`(?i)\bsys\.[A-Za-z_]`)
)

// NormalizeMSSQLConstraintNames canonicalizes auto-generated constraint
// names in MSSQL dump output. See the package comment above for the
// naming scheme and residual risks.
func NormalizeMSSQLConstraintNames(sqlText string) string {
	// SQL Server escapes ] as ]] inside bracketed identifiers and ' as ''
	// inside string literals. Decode before matching so names containing
	// those characters are found; re-escape when writing back.
	decode := func(s string) string {
		return strings.ReplaceAll(strings.ReplaceAll(s, "]]", "]"), "''", "'")
	}

	// Collect every distinct auto-generated name (decoded form) together
	// with the byte offset of its first occurrence, plus the set of all
	// explicitly named identifiers that must stay reserved. Reservation
	// covers both quoting forms: a name that only ever appears as a
	// literal still occupies the constraint namespace.
	autoFirstPos := make(map[string]int)
	used := make(map[string]bool)
	collect := func(name string, pos int) {
		name = decode(name)
		if autoConstraintName.MatchString(name) {
			if _, ok := autoFirstPos[name]; !ok {
				autoFirstPos[name] = pos
			}
			return
		}
		used[name] = true
	}
	for _, m := range quotedNameBrackets.FindAllStringSubmatchIndex(sqlText, -1) {
		collect(sqlText[m[2]:m[3]], m[0])
	}
	for _, m := range quotedNameLiteral.FindAllStringSubmatchIndex(sqlText, -1) {
		collect(sqlText[m[4]:m[5]], m[0])
	}
	if len(autoFirstPos) == 0 {
		return sqlText
	}

	// Order candidates by first occurrence in dump text, not by the
	// random tail: emission order is fixed, so canonical assignment is
	// stable even when two scratch databases swap random tails.
	names := make([]string, 0, len(autoFirstPos))
	for n := range autoFirstPos {
		names = append(names, n)
	}
	sort.Slice(names, func(i, j int) bool {
		pi, pj := autoFirstPos[names[i]], autoFirstPos[names[j]]
		if pi != pj {
			return pi < pj
		}
		return names[i] < names[j]
	})

	renames := make(map[string]string, len(names))
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

	// Rewrite bracketed occurrences. The match spans [name]; group 1 is
	// the raw (still escaped) content.
	var b strings.Builder
	b.Grow(len(sqlText))
	last := 0
	for _, m := range quotedNameBrackets.FindAllStringSubmatchIndex(sqlText, -1) {
		name := decode(sqlText[m[2]:m[3]])
		newName, ok := renames[name]
		if !ok {
			continue
		}
		b.WriteString(sqlText[last:m[0]])
		b.WriteByte('[')
		b.WriteString(strings.ReplaceAll(newName, "]", "]]"))
		b.WriteByte(']')
		last = m[1]
	}
	b.WriteString(sqlText[last:])
	sqlText = b.String()

	// Rewrite literal occurrences — but only where the literal is really
	// a constraint identifier: a name = N'...' catalog-probe predicate or
	// a CONSTRAINT clause. Quoted auto-name-shaped data (e.g. a DEFAULT
	// constant) is left untouched. Group 1 is the N? prefix, group 2 the
	// raw (still escaped) content.
	b.Reset()
	b.Grow(len(sqlText))
	last = 0
	for _, m := range quotedNameLiteral.FindAllStringSubmatchIndex(sqlText, -1) {
		name := decode(sqlText[m[4]:m[5]])
		newName, ok := renames[name]
		if !ok {
			continue
		}
		// The literal must sit in a constraint-identifier context: a
		// name = N'...' predicate inside a sys.* catalog probe within the
		// same GO batch, or directly after the CONSTRAINT keyword.
		tailStart := m[0] - 64
		if tailStart < 0 {
			tailStart = 0
		}
		tail := sqlText[tailStart:m[0]]
		isNamePredicate := literalNameEquals.MatchString(tail)
		isConstraintClause := literalConstraintTail.MatchString(tail)
		if !isNamePredicate && !isConstraintClause {
			continue
		}
		if isNamePredicate {
			// Catalog probes reference sys.* views; require one earlier
			// in the same GO batch (bounded window) so data literals in
			// CHECK/DEFAULT clauses are not rewritten.
			winStart := m[0] - 384
			if winStart < 0 {
				winStart = 0
			}
			stmtStart := 0
			for _, sep := range []string{"\nGO\r\n", "\nGO\n"} {
				if idx := strings.LastIndex(sqlText[:m[0]], sep); idx >= 0 && idx+len(sep) > stmtStart {
					stmtStart = idx + len(sep)
				}
			}
			if stmtStart > winStart {
				winStart = stmtStart
			}
			if !literalCatalogProbe.MatchString(sqlText[winStart:m[0]]) {
				continue
			}
		}
		b.WriteString(sqlText[last:m[0]])
		b.WriteString(sqlText[m[2]:m[4]]) // N?' opening quote
		b.WriteString(strings.ReplaceAll(newName, "'", "''"))
		b.WriteByte('\'')
		last = m[1]
	}
	b.WriteString(sqlText[last:])
	return b.String()
}

// canonicalHex derives the deterministic 8-hex tail from the stable
// prefix, mirroring SQL Server's own hex-suffix shape.
func canonicalHex(prefix string) string {
	h := fnv.New32a()
	h.Write([]byte(strings.ToLower(prefix)))
	return fmt.Sprintf("%08X", h.Sum32())
}
