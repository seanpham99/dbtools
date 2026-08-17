package ddlcheck

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

// ObjectRef names one database object a migration's DDL creates.
type ObjectRef struct {
	Schema string
	Name   string
	Kind   string // "table", "procedure", "view", or "function"
}

// createObjectPattern matches a top-level "CREATE [OR ALTER]
// TABLE|PROCEDURE|VIEW|FUNCTION [schema].[name]" statement, with or
// without bracket-quoted identifiers. It deliberately does not match
// CREATE INDEX (out of scope — see design spec) or any ALTER-only
// statement (ALTER TABLE ADD COLUMN introduces no new named object).
var createObjectPattern = regexp.MustCompile(
	`(?im)^\s*CREATE\s+(?:OR\s+ALTER\s+)?(TABLE|PROCEDURE|PROC|VIEW|FUNCTION)\s+` +
		`(?:\[?([A-Za-z_][A-Za-z0-9_]*)\]?\.)?\[?([A-Za-z_][A-Za-z0-9_]*)\]?`)

var kindNames = map[string]string{
	"TABLE":     "table",
	"PROCEDURE": "procedure",
	"PROC":      "procedure",
	"VIEW":      "view",
	"FUNCTION":  "function",
}

// ExtractObjects scans sqlText for top-level CREATE [OR ALTER]
// TABLE/PROCEDURE/VIEW/FUNCTION statements and returns the object each one
// names, in source order. Column-level changes (e.g. ALTER TABLE ... ADD)
// introduce no new named object and are not represented — see the design
// spec's "known gap" for why this is an accepted limitation.
func ExtractObjects(sqlText string) []ObjectRef {
	matches := createObjectPattern.FindAllStringSubmatch(sqlText, -1)
	objects := make([]ObjectRef, 0, len(matches))
	for _, m := range matches {
		schema := m[2]
		if schema == "" {
			schema = "dbo"
		}
		objects = append(objects, ObjectRef{
			Schema: schema,
			Name:   m[3],
			Kind:   kindNames[strings.ToUpper(m[1])],
		})
	}
	return objects
}

// dropObjectPattern mirrors createObjectPattern for DROP statements, used
// to recognize when a later migration intentionally removes an object an
// earlier one created (see internal/verify — a CREATEd-then-DROPped object
// is expected-absent, not drift).
var dropObjectPattern = regexp.MustCompile(
	`(?im)^\s*DROP\s+(TABLE|PROCEDURE|PROC|VIEW|FUNCTION)\s+` +
		`(?:\[?([A-Za-z_][A-Za-z0-9_]*)\]?\.)?\[?([A-Za-z_][A-Za-z0-9_]*)\]?`)

// ExtractDroppedObjects scans sqlText for top-level DROP
// TABLE/PROCEDURE/VIEW/FUNCTION statements and returns the object each one
// names, in source order. Mirrors ExtractObjects's schema-defaulting and
// kind-normalization exactly.
func ExtractDroppedObjects(sqlText string) []ObjectRef {
	matches := dropObjectPattern.FindAllStringSubmatch(sqlText, -1)
	objects := make([]ObjectRef, 0, len(matches))
	for _, m := range matches {
		schema := m[2]
		if schema == "" {
			schema = "dbo"
		}
		objects = append(objects, ObjectRef{
			Schema: schema,
			Name:   m[3],
			Kind:   kindNames[strings.ToUpper(m[1])],
		})
	}
	return objects
}

// Exists reports whether ref currently exists in db.
func Exists(db *sql.DB, ref ObjectRef) (bool, error) {
	var typeFilter string
	switch ref.Kind {
	case "table":
		typeFilter = "o.type = 'U'"
	case "procedure":
		typeFilter = "o.type IN ('P', 'PC')"
	case "view":
		typeFilter = "o.type = 'V'"
	case "function":
		typeFilter = "o.type IN ('FN', 'IF', 'TF', 'FS', 'FT')"
	default:
		return false, fmt.Errorf("unknown object kind %q", ref.Kind)
	}

	// typeFilter is always one of the fixed literals above (the switch's
	// default case returns before this point for anything else) — never
	// derived from caller input, so this Sprintf is safe.
	query := fmt.Sprintf(`
SELECT COUNT(*)
FROM sys.objects o
JOIN sys.schemas s ON o.schema_id = s.schema_id
WHERE s.name = @p1 AND o.name = @p2 AND (%s)`, typeFilter)

	var count int
	if err := db.QueryRow(query, ref.Schema, ref.Name).Scan(&count); err != nil {
		return false, fmt.Errorf("checking existence of %s.%s: %w", ref.Schema, ref.Name, err)
	}
	return count > 0, nil
}
