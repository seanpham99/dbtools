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

// renameObjectPattern matches a top-level ALTER TABLE … RENAME TO
// statement, the last step of a rebuild in place. SQLite names the target
// with a bare identifier and leaves the table in its schema, so the target
// carries the source's schema. RENAME COLUMN does not match, because a
// column is not a tracked object.
var renameObjectPattern = regexp.MustCompile(
	`(?im)^\s*ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:` + identifier + `\.)?` + identifier +
		`\s+RENAME\s+TO\s+` + identifier)

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
// each at the offset of the statement that performs it. A rename contributes
// two operations at one offset: the name it takes away and the name it
// produces.
func (ddl) ExtractOperations(sqlText string) []ddlcheck.Operation {
	ops := make([]ddlcheck.Operation, 0, len(sqlText)/64+4)
	for _, loc := range createObjectPattern.FindAllStringSubmatchIndex(sqlText, -1) {
		ops = append(ops, ddlcheck.Operation{Kind: ddlcheck.OpCreate, Ref: refFromLoc(sqlText, loc), Pos: loc[0]})
	}
	for _, loc := range dropObjectPattern.FindAllStringSubmatchIndex(sqlText, -1) {
		ops = append(ops, ddlcheck.Operation{Kind: ddlcheck.OpDrop, Ref: refFromLoc(sqlText, loc), Pos: loc[0]})
	}
	for _, loc := range renameObjectPattern.FindAllStringSubmatchIndex(sqlText, -1) {
		schema := ddlcheck.Submatch(sqlText, loc, 1)
		if schema == "" {
			schema = DefaultSchema
		}
		ops = append(ops,
			ddlcheck.Operation{
				Kind: ddlcheck.OpRenameFrom,
				Ref:  ddlcheck.ObjectRef{Schema: schema, Name: ddlcheck.Submatch(sqlText, loc, 2), Kind: "table"},
				Pos:  loc[0],
			},
			ddlcheck.Operation{
				Kind: ddlcheck.OpRenameTo,
				Ref:  ddlcheck.ObjectRef{Schema: schema, Name: ddlcheck.Submatch(sqlText, loc, 3), Kind: "table"},
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

// ExtractRenamedObjects returns one Rename per ALTER TABLE … RENAME TO
// statement, in source order.
func (d ddl) ExtractRenamedObjects(sqlText string) []ddlcheck.Rename {
	return ddlcheck.RenamesOf(d.ExtractOperations(sqlText))
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
