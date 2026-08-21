package sqliteengine

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"github.com/seanpham99/dbtools/internal/ddlcheck"
)

// DefaultSchema is SQLite's name for the primary database.
const DefaultSchema = "main"

// identifier matches an optionally double-quoted or bracket/backtick-free
// SQLite identifier. SQLite also accepts [name] and `name`, but dbtools
// migrations use plain or double-quoted names, matching the other dialects.
const identifier = `"?([A-Za-z_][A-Za-z0-9_]*)"?`

// createObjectPattern matches a top-level CREATE [TEMP|TEMPORARY]
// TABLE|VIEW [IF NOT EXISTS] [schema.]name statement. SQLite has no
// stored procedures or functions, so those kinds simply never occur;
// CREATE INDEX/TRIGGER are out of scope, mirroring the other dialects.
var createObjectPattern = regexp.MustCompile(
	`(?im)^\s*CREATE\s+(?:TEMP\s+|TEMPORARY\s+)?(TABLE|VIEW)\s+(?:IF\s+NOT\s+EXISTS\s+)?` +
		`(?:` + identifier + `\.)?` + identifier)

var dropObjectPattern = regexp.MustCompile(
	`(?im)^\s*DROP\s+(TABLE|VIEW)\s+(?:IF\s+EXISTS\s+)?` +
		`(?:` + identifier + `\.)?` + identifier)

func refsFrom(matches [][]string) []ddlcheck.ObjectRef {
	objects := make([]ddlcheck.ObjectRef, 0, len(matches))
	for _, m := range matches {
		schema := m[2]
		if schema == "" {
			schema = DefaultSchema
		}
		objects = append(objects, ddlcheck.ObjectRef{
			Schema: schema,
			Name:   m[3],
			Kind:   strings.ToLower(m[1]),
		})
	}
	return objects
}

type ddl struct{}

// ExtractObjects returns the objects sqlText's top-level CREATE statements
// name, in source order.
func (ddl) ExtractObjects(sqlText string) []ddlcheck.ObjectRef {
	return refsFrom(createObjectPattern.FindAllStringSubmatch(sqlText, -1))
}

// ExtractDroppedObjects mirrors ExtractObjects for DROP statements.
func (ddl) ExtractDroppedObjects(sqlText string) []ddlcheck.ObjectRef {
	return refsFrom(dropObjectPattern.FindAllStringSubmatch(sqlText, -1))
}

// Exists reports whether ref currently exists in db, via sqlite_master.
func (ddl) Exists(db *sql.DB, ref ddlcheck.ObjectRef) (bool, error) {
	var objType string
	switch ref.Kind {
	case "table":
		objType = "table"
	case "view":
		objType = "view"
	default:
		return false, fmt.Errorf("unknown object kind %q", ref.Kind)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = ? AND name = ?`, objType, ref.Name).Scan(&count); err != nil {
		return false, fmt.Errorf("checking existence of %s.%s: %w", ref.Schema, ref.Name, err)
	}
	return count > 0, nil
}
