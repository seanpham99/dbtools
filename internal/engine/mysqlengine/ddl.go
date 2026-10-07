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

// renameObjectPattern matches a top-level ALTER TABLE|VIEW … RENAME
// TO|AS statement, the last step of a rebuild in place. Both sides may name
// a database. The standalone RENAME TABLE form is deliberately out of
// scope: it renames a comma-separated list, and this engine's extractors
// do not mask stored-program bodies, so a partial match would be worse
// than none.
var renameObjectPattern = regexp.MustCompile(
	`(?im)^\s*ALTER\s+(TABLE|VIEW)\s+(?:IF\s+EXISTS\s+)?(?:` + identifier + `\.)?` + identifier +
		`\s+RENAME\s+(?:TO|AS)\s+(?:` + identifier + `\.)?` + identifier)

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

// ExtractRenamedObjects returns one Rename per ALTER … RENAME TO|AS
// statement, in source order. An unqualified target keeps the source's
// database, which is what MySQL does when the statement does not name one.
func (mysqlDDL) ExtractRenamedObjects(sqlText string) []ddlcheck.Rename {
	matches := renameObjectPattern.FindAllStringSubmatch(sqlText, -1)
	renames := make([]ddlcheck.Rename, 0, len(matches))
	for _, m := range matches {
		target := m[4]
		if target == "" {
			target = m[2]
		}
		renames = append(renames, ddlcheck.Rename{
			From: ddlcheck.ObjectRef{Schema: m[2], Name: m[3], Kind: strings.ToLower(m[1])},
			To:   ddlcheck.ObjectRef{Schema: target, Name: m[5], Kind: strings.ToLower(m[1])},
		})
	}
	return renames
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
