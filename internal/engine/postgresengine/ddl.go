package postgresengine

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"github.com/seanpham99/dbtools/internal/ddlcheck"
)

// DefaultSchema is where unqualified Postgres object names live.
const DefaultSchema = "public"

// maskNonExecutable replaces the contents of every non-executable region
// of sqlText — dollar-quoted strings (function/procedure bodies), ordinary
// and escape string literals, double-quoted identifiers, line comments,
// and block comments — with spaces, preserving newlines so the extractors'
// ^-anchored, position-independent regexes still see the same line
// structure. This is a small lexical scanner, not a regex pass: dollar
// tags follow Postgres identifier rules ($$, or $tag$ where tag is
// [A-Za-z_][A-Za-z0-9_]*), a `$tag$` inside a string or comment does not
// open a body, and an unterminated region masks to the end of the text —
// the safe direction for drift detection, since it can only under-report
// objects inside a body, never invent them.
func maskNonExecutable(sqlText string) string {
	src := []byte(sqlText)
	out := make([]byte, len(src))
	copy(out, src)

	blank := func(from, to int) {
		for i := from; i < to && i < len(out); i++ {
			if out[i] != '\n' {
				out[i] = ' '
			}
		}
	}

	// dollarTagAt reports the full $tag$ opener starting at i, or "" if
	// src[i:] does not start a valid dollar-quote tag.
	dollarTagAt := func(i int) string {
		if i >= len(src) || src[i] != '$' {
			return ""
		}
		j := i + 1
		if j < len(src) && (isIdentStart(src[j])) {
			j++
			for j < len(src) && isIdentPart(src[j]) {
				j++
			}
		}
		if j < len(src) && src[j] == '$' {
			return string(src[i : j+1])
		}
		return ""
	}

	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '-' && i+1 < len(src) && src[i+1] == '-': // line comment
			end := strings.IndexByte(string(src[i:]), '\n')
			if end < 0 {
				blank(i, len(src))
				return string(out)
			}
			blank(i, i+end)
			i += end + 1
		case c == '/' && i+1 < len(src) && src[i+1] == '*': // block comment (nestable in Postgres)
			depth := 1
			j := i + 2
			for j < len(src) && depth > 0 {
				if j+1 < len(src) && src[j] == '/' && src[j+1] == '*' {
					depth++
					j += 2
				} else if j+1 < len(src) && src[j] == '*' && src[j+1] == '/' {
					depth--
					j += 2
				} else {
					j++
				}
			}
			blank(i, j)
			i = j
		case c == '\'' || (i+1 < len(src) && (c == 'E' || c == 'e') && src[i+1] == '\''): // string literal
			start := i
			if c != '\'' {
				i++ // skip the E of an escape string
			}
			escape := c != '\''
			j := i + 1
			for j < len(src) {
				if escape && src[j] == '\\' {
					j += 2
					continue
				}
				if src[j] == '\'' {
					if j+1 < len(src) && src[j+1] == '\'' { // doubled quote
						j += 2
						continue
					}
					j++
					break
				}
				j++
			}
			blank(start, j)
			i = j
		case c == '"': // quoted identifier — mask the quotes' interior only,
			// keeping the name visible to the extractors.
			j := i + 1
			for j < len(src) {
				if src[j] == '"' {
					if j+1 < len(src) && src[j+1] == '"' {
						j += 2
						continue
					}
					j++
					break
				}
				j++
			}
			i = j
		case c == '$':
			if tag := dollarTagAt(i); tag != "" {
				rest := strings.Index(string(src[i+len(tag):]), tag)
				if rest < 0 {
					blank(i, len(src))
					return string(out)
				}
				end := i + len(tag) + rest + len(tag)
				blank(i, end)
				i = end
			} else {
				i++
			}
		default:
			i++
		}
	}
	return string(out)
}

func isIdentStart(b byte) bool {
	return b == '_' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func isIdentPart(b byte) bool {
	return isIdentStart(b) || (b >= '0' && b <= '9')
}

// identifier matches an optionally double-quoted Postgres identifier.
const identifier = `"?([A-Za-z_][A-Za-z0-9_]*)"?`

// createObjectPattern matches a top-level CREATE [OR REPLACE]
// TABLE|VIEW|FUNCTION|PROCEDURE [IF NOT EXISTS] [schema.]name statement.
// CREATE INDEX is out of scope, mirroring the MSSQL dialect; so is ALTER,
// except for ALTER … RENAME TO, which moves an object from one tracked
// name to another and so is part of what a file leaves behind.
// UNLOGGED/TEMP tables and MATERIALIZED views are matched through their
// optional modifier keywords.
var createObjectPattern = regexp.MustCompile(
	`(?im)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?(?:UNLOGGED\s+|TEMP\s+|TEMPORARY\s+|MATERIALIZED\s+)?` +
		`(TABLE|VIEW|FUNCTION|PROCEDURE)\s+(?:IF\s+NOT\s+EXISTS\s+)?` +
		`(?:` + identifier + `\.)?` + identifier)

var dropObjectPattern = regexp.MustCompile(
	`(?im)^\s*DROP\s+(?:MATERIALIZED\s+)?(TABLE|VIEW|FUNCTION|PROCEDURE)\s+(?:IF\s+EXISTS\s+)?` +
		`(?:` + identifier + `\.)?` + identifier)

// renameObjectPattern matches a top-level ALTER TABLE|VIEW … RENAME TO
// statement, the last step of a rebuild in place. Postgres names the target
// with a bare identifier and leaves the object in its schema, so the target
// carries the source's schema.
var renameObjectPattern = regexp.MustCompile(
	`(?im)^\s*ALTER\s+(?:MATERIALIZED\s+)?(TABLE|VIEW)\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?` +
		`(?:` + identifier + `\.)?` + identifier + `\s+RENAME\s+TO\s+` + identifier)

// refFromLoc builds an ObjectRef from a create- or drop-pattern match
// located in text, whose capture groups are kind, schema, then name.
func refFromLoc(text string, loc []int) ddlcheck.ObjectRef {
	schema := ddlcheck.Submatch(text, loc, 2)
	if schema == "" {
		schema = DefaultSchema
	}
	return ddlcheck.ObjectRef{
		Schema: schema,
		Name:   ddlcheck.Submatch(text, loc, 3),
		Kind:   strings.ToLower(ddlcheck.Submatch(text, loc, 1)),
	}
}

type ddl struct{}

// ExtractOperations returns every object-naming operation sqlText contains,
// each at the offset of the statement that performs it, ignoring anything
// inside dollar-quoted bodies. A rename contributes two operations at one
// offset: the name it takes away and the name it produces.
func (ddl) ExtractOperations(sqlText string) []ddlcheck.Operation {
	masked := maskNonExecutable(sqlText)
	ops := make([]ddlcheck.Operation, 0, len(sqlText)/64+4)
	for _, loc := range createObjectPattern.FindAllStringSubmatchIndex(masked, -1) {
		ops = append(ops, ddlcheck.Operation{Kind: ddlcheck.OpCreate, Ref: refFromLoc(masked, loc), Pos: loc[0]})
	}
	for _, loc := range dropObjectPattern.FindAllStringSubmatchIndex(masked, -1) {
		ops = append(ops, ddlcheck.Operation{Kind: ddlcheck.OpDrop, Ref: refFromLoc(masked, loc), Pos: loc[0]})
	}
	for _, loc := range renameObjectPattern.FindAllStringSubmatchIndex(masked, -1) {
		schema := ddlcheck.Submatch(masked, loc, 2)
		if schema == "" {
			schema = DefaultSchema
		}
		kind := strings.ToLower(ddlcheck.Submatch(masked, loc, 1))
		ops = append(ops,
			ddlcheck.Operation{
				Kind: ddlcheck.OpRenameFrom,
				Ref:  ddlcheck.ObjectRef{Schema: schema, Name: ddlcheck.Submatch(masked, loc, 3), Kind: kind},
				Pos:  loc[0],
			},
			ddlcheck.Operation{
				Kind: ddlcheck.OpRenameTo,
				Ref:  ddlcheck.ObjectRef{Schema: schema, Name: ddlcheck.Submatch(masked, loc, 4), Kind: kind},
				Pos:  loc[0],
			},
		)
	}
	return ops
}

// ExtractObjects returns the objects sqlText's top-level CREATE statements
// name, in source order.
func (d ddl) ExtractObjects(sqlText string) []ddlcheck.ObjectRef {
	return ddlcheck.RefsOf(d.ExtractOperations(sqlText), ddlcheck.OpCreate)
}

// ExtractDroppedObjects mirrors ExtractObjects for DROP statements.
func (d ddl) ExtractDroppedObjects(sqlText string) []ddlcheck.ObjectRef {
	return ddlcheck.RefsOf(d.ExtractOperations(sqlText), ddlcheck.OpDrop)
}

// ExtractRenamedObjects returns one Rename per ALTER … RENAME TO
// statement, in source order.
func (d ddl) ExtractRenamedObjects(sqlText string) []ddlcheck.Rename {
	return ddlcheck.RenamesOf(d.ExtractOperations(sqlText))
}

// Exists reports whether ref currently exists in db.
func (ddl) Exists(db *sql.DB, ref ddlcheck.ObjectRef) (bool, error) {
	var query string
	switch ref.Kind {
	case "table":
		// r = ordinary, p = partitioned, u = unlogged is still 'r'
		query = `SELECT COUNT(*) FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON c.relnamespace = n.oid
WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind IN ('r', 'p')`
	case "view":
		// v = view, m = materialized view
		query = `SELECT COUNT(*) FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON c.relnamespace = n.oid
WHERE n.nspname = $1 AND c.relname = $2 AND c.relkind IN ('v', 'm')`
	case "function":
		// f = function, w = window function; aggregates excluded on purpose
		query = `SELECT COUNT(*) FROM pg_catalog.pg_proc p
JOIN pg_catalog.pg_namespace n ON p.pronamespace = n.oid
WHERE n.nspname = $1 AND p.proname = $2 AND p.prokind IN ('f', 'w')`
	case "procedure":
		query = `SELECT COUNT(*) FROM pg_catalog.pg_proc p
JOIN pg_catalog.pg_namespace n ON p.pronamespace = n.oid
WHERE n.nspname = $1 AND p.proname = $2 AND p.prokind = 'p'`
	default:
		return false, fmt.Errorf("unknown object kind %q", ref.Kind)
	}

	var count int
	if err := db.QueryRow(query, ref.Schema, ref.Name).Scan(&count); err != nil {
		return false, fmt.Errorf("checking existence of %s.%s: %w", ref.Schema, ref.Name, err)
	}
	return count > 0, nil
}
