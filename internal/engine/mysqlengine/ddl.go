package mysqlengine

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"github.com/seanpham99/dbtools/internal/ddlcheck"
)

// identifier matches an optionally backtick-quoted MySQL identifier.
const identifier = "`?([A-Za-z_][A-Za-z0-9_]*)`?"

// createObjectPattern matches a top-level CREATE [OR REPLACE] [TEMPORARY]
// TABLE|VIEW [IF NOT EXISTS] [db.]name statement. See the package's DDL
// scope note (docs/superpowers/plans/2026-08-25-mysql-engine.md, Task 4):
// procedures/functions are out of scope.
var createObjectPattern = regexp.MustCompile(
	`(?im)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?(?:TEMPORARY\s+)?(TABLE|VIEW)\s+(?:IF\s+NOT\s+EXISTS\s+)?` +
		`(?:` + identifier + `\.)?` + identifier)

var dropObjectPattern = regexp.MustCompile(
	`(?im)^\s*DROP\s+(TABLE|VIEW)\s+(?:IF\s+EXISTS\s+)?` +
		`(?:` + identifier + `\.)?` + identifier)

func refsFrom(matches [][]string) []ddlcheck.ObjectRef {
	objects := make([]ddlcheck.ObjectRef, 0, len(matches))
	for _, m := range matches {
		objects = append(objects, ddlcheck.ObjectRef{
			// Schema stays "" for the common unqualified case — see the
			// package-level design note on why MySQL has no fixed default
			// schema literal the way dbo/public/main are for the other
			// three dialects.
			Schema: m[2],
			Name:   m[3],
			Kind:   strings.ToLower(m[1]),
		})
	}
	return objects
}

type mysqlDDL struct{}

func (mysqlDDL) ExtractObjects(sqlText string) []ddlcheck.ObjectRef {
	return refsFrom(createObjectPattern.FindAllStringSubmatch(sqlText, -1))
}

func (mysqlDDL) ExtractDroppedObjects(sqlText string) []ddlcheck.ObjectRef {
	return refsFrom(dropObjectPattern.FindAllStringSubmatch(sqlText, -1))
}

// Exists reports whether ref currently exists in db. An empty ref.Schema
// means "the database this connection is currently using" (DATABASE()).
func (mysqlDDL) Exists(db *sql.DB, ref ddlcheck.ObjectRef) (bool, error) {
	var typeFilter string
	switch ref.Kind {
	case "table":
		typeFilter = "TABLE_TYPE = 'BASE TABLE'"
	case "view":
		typeFilter = "TABLE_TYPE = 'VIEW'"
	default:
		return false, fmt.Errorf("unknown object kind %q", ref.Kind)
	}

	schemaClause := "TABLE_SCHEMA = DATABASE()"
	args := []any{ref.Name}
	if ref.Schema != "" {
		schemaClause = "TABLE_SCHEMA = ?"
		args = []any{ref.Schema, ref.Name}
	}

	query := fmt.Sprintf(`
SELECT COUNT(*)
FROM information_schema.tables
WHERE %s AND TABLE_NAME = ? AND (%s)`, schemaClause, typeFilter)

	var count int
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		return false, fmt.Errorf("checking existence of %s.%s: %w", ref.Schema, ref.Name, err)
	}
	return count > 0, nil
}
