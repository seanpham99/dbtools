package mssqlengine

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"github.com/seanpham99/dbtools/internal/ddlcheck"
	"github.com/seanpham99/dbtools/internal/engine"
)

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

// refFromLoc builds an ObjectRef from a create- or drop-pattern match
// located in text, whose capture groups are kind, schema, then name.
func refFromLoc(text string, loc []int) ddlcheck.ObjectRef {
	schema := ddlcheck.Submatch(text, loc, 2)
	if schema == "" {
		schema = "dbo"
	}
	return ddlcheck.ObjectRef{
		Schema: schema,
		Name:   ddlcheck.Submatch(text, loc, 3),
		Kind:   kindNames[strings.ToUpper(ddlcheck.Submatch(text, loc, 1))],
	}
}

// ExtractObjects scans sqlText for top-level CREATE statements.
func ExtractObjects(sqlText string) []ddlcheck.ObjectRef {
	return ddlcheck.RefsOf(ExtractOperations(sqlText), ddlcheck.OpCreate)
}

var dropObjectPattern = regexp.MustCompile(
	`(?im)^\s*DROP\s+(TABLE|PROCEDURE|PROC|VIEW|FUNCTION)\s+` +
		`(?:\[?([A-Za-z_][A-Za-z0-9_]*)\]?\.)?\[?([A-Za-z_][A-Za-z0-9_]*)\]?`)

// ExtractDroppedObjects scans sqlText for top-level DROP statements.
func ExtractDroppedObjects(sqlText string) []ddlcheck.ObjectRef {
	return ddlcheck.RefsOf(ExtractOperations(sqlText), ddlcheck.OpDrop)
}

// Exists reports whether ref currently exists in MSSQL db.
func Exists(db *sql.DB, ref ddlcheck.ObjectRef) (bool, error) {
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

type mssqlDDL struct{}

// ExtractOperations returns every object-naming operation sqlText contains,
// each at the offset of the statement that performs it. It never returns a
// rename, for the reason ExtractRenamedObjects gives.
func ExtractOperations(sqlText string) []ddlcheck.Operation {
	ops := make([]ddlcheck.Operation, 0, len(sqlText)/64+4)
	for _, loc := range createObjectPattern.FindAllStringSubmatchIndex(sqlText, -1) {
		ops = append(ops, ddlcheck.Operation{Kind: ddlcheck.OpCreate, Ref: refFromLoc(sqlText, loc), Pos: loc[0]})
	}
	for _, loc := range dropObjectPattern.FindAllStringSubmatchIndex(sqlText, -1) {
		ops = append(ops, ddlcheck.Operation{Kind: ddlcheck.OpDrop, Ref: refFromLoc(sqlText, loc), Pos: loc[0]})
	}
	return ops
}

func (mssqlDDL) ExtractOperations(sqlText string) []ddlcheck.Operation {
	return ExtractOperations(sqlText)
}

func (mssqlDDL) ExtractObjects(sqlText string) []ddlcheck.ObjectRef {
	return ExtractObjects(sqlText)
}

func (mssqlDDL) ExtractDroppedObjects(sqlText string) []ddlcheck.ObjectRef {
	return ExtractDroppedObjects(sqlText)
}

// ExtractRenamedObjects returns nothing: SQL Server renames through
// sp_rename, a stored procedure called with string-literal arguments
// ('object', 'new_name' [, 'OBJECT']), and there is no ALTER … RENAME
// statement to read. Parsing the call would mean guessing at how each
// argument is quoted and whether the third argument names the object kind,
// so this dialect reports no renames instead. A rebuild-in-place migration
// on MSSQL therefore still needs `dbtools repair --force` for the
// temporary name, and verify still reports that name as drift.
func (mssqlDDL) ExtractRenamedObjects(sqlText string) []ddlcheck.Rename { return nil }

func (mssqlDDL) Exists(db *sql.DB, ref ddlcheck.ObjectRef) (bool, error) {
	return Exists(db, ref)
}

var _ engine.DDLDialect = mssqlDDL{}
