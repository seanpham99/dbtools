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

// refFromLoc builds an ObjectRef from a create- or drop-pattern match
// located in text, whose capture groups are kind, database, then name.
func refFromLoc(text string, loc []int) ddlcheck.ObjectRef {
	return ddlcheck.ObjectRef{
		// Schema stays "" for the common unqualified case — see the
		// package-level design note on why MySQL has no fixed default
		// schema literal the way dbo/public/main are for the other three
		// dialects.
		Schema: ddlcheck.Submatch(text, loc, 2),
		Name:   ddlcheck.Submatch(text, loc, 3),
		Kind:   strings.ToLower(ddlcheck.Submatch(text, loc, 1)),
	}
}

type mysqlDDL struct{}

// ExtractOperations returns every object-naming operation sqlText contains,
// each at the offset of the statement that performs it. A rename contributes
// two operations at one offset: the name it takes away and the name it
// produces.
func (mysqlDDL) ExtractOperations(sqlText string) []ddlcheck.Operation {
	ops := make([]ddlcheck.Operation, 0, len(sqlText)/64+4)
	for _, loc := range createObjectPattern.FindAllStringSubmatchIndex(sqlText, -1) {
		ops = append(ops, ddlcheck.Operation{Kind: ddlcheck.OpCreate, Ref: refFromLoc(sqlText, loc), Pos: loc[0]})
	}
	for _, loc := range dropObjectPattern.FindAllStringSubmatchIndex(sqlText, -1) {
		ops = append(ops, ddlcheck.Operation{Kind: ddlcheck.OpDrop, Ref: refFromLoc(sqlText, loc), Pos: loc[0]})
	}
	for _, loc := range renameObjectPattern.FindAllStringSubmatchIndex(sqlText, -1) {
		kind := strings.ToLower(ddlcheck.Submatch(sqlText, loc, 1))
		target := ddlcheck.Submatch(sqlText, loc, 4)
		if target == "" {
			target = ddlcheck.Submatch(sqlText, loc, 2)
		}
		ops = append(ops,
			ddlcheck.Operation{
				Kind: ddlcheck.OpRenameFrom,
				Ref:  ddlcheck.ObjectRef{Schema: ddlcheck.Submatch(sqlText, loc, 2), Name: ddlcheck.Submatch(sqlText, loc, 3), Kind: kind},
				Pos:  loc[0],
			},
			ddlcheck.Operation{
				Kind: ddlcheck.OpRenameTo,
				Ref:  ddlcheck.ObjectRef{Schema: target, Name: ddlcheck.Submatch(sqlText, loc, 5), Kind: kind},
				Pos:  loc[0],
			},
		)
	}
	return ops
}

// ExtractObjects returns the objects sqlText's top-level CREATE statements
// name, in source order.
func (d mysqlDDL) ExtractObjects(sqlText string) []ddlcheck.ObjectRef {
	return ddlcheck.RefsOf(d.ExtractOperations(sqlText), ddlcheck.OpCreate)
}

// ExtractDroppedObjects mirrors ExtractObjects for DROP statements.
func (d mysqlDDL) ExtractDroppedObjects(sqlText string) []ddlcheck.ObjectRef {
	return ddlcheck.RefsOf(d.ExtractOperations(sqlText), ddlcheck.OpDrop)
}

// ExtractRenamedObjects returns one Rename per ALTER … RENAME TO|AS
// statement, in source order. An unqualified target keeps the source's
// database, which is what MySQL does when the statement does not name one.
func (d mysqlDDL) ExtractRenamedObjects(sqlText string) []ddlcheck.Rename {
	return ddlcheck.RenamesOf(d.ExtractOperations(sqlText))
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
