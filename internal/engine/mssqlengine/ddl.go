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

// ExtractObjects scans sqlText for top-level CREATE statements.
func ExtractObjects(sqlText string) []ddlcheck.ObjectRef {
	matches := createObjectPattern.FindAllStringSubmatch(sqlText, -1)
	objects := make([]ddlcheck.ObjectRef, 0, len(matches))
	for _, m := range matches {
		schema := m[2]
		if schema == "" {
			schema = "dbo"
		}
		objects = append(objects, ddlcheck.ObjectRef{
			Schema: schema,
			Name:   m[3],
			Kind:   kindNames[strings.ToUpper(m[1])],
		})
	}
	return objects
}

var dropObjectPattern = regexp.MustCompile(
	`(?im)^\s*DROP\s+(TABLE|PROCEDURE|PROC|VIEW|FUNCTION)\s+` +
		`(?:\[?([A-Za-z_][A-Za-z0-9_]*)\]?\.)?\[?([A-Za-z_][A-Za-z0-9_]*)\]?`)

// ExtractDroppedObjects scans sqlText for top-level DROP statements.
func ExtractDroppedObjects(sqlText string) []ddlcheck.ObjectRef {
	matches := dropObjectPattern.FindAllStringSubmatch(sqlText, -1)
	objects := make([]ddlcheck.ObjectRef, 0, len(matches))
	for _, m := range matches {
		schema := m[2]
		if schema == "" {
			schema = "dbo"
		}
		objects = append(objects, ddlcheck.ObjectRef{
			Schema: schema,
			Name:   m[3],
			Kind:   kindNames[strings.ToUpper(m[1])],
		})
	}
	return objects
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

func (mssqlDDL) ExtractObjects(sqlText string) []ddlcheck.ObjectRef {
	return ExtractObjects(sqlText)
}

func (mssqlDDL) ExtractDroppedObjects(sqlText string) []ddlcheck.ObjectRef {
	return ExtractDroppedObjects(sqlText)
}

func (mssqlDDL) Exists(db *sql.DB, ref ddlcheck.ObjectRef) (bool, error) {
	return Exists(db, ref)
}

var _ engine.DDLDialect = mssqlDDL{}
