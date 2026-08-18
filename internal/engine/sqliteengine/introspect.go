package sqliteengine

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/dbtools/dbtools/internal/generate"
)

// MapSQLiteToPython maps a SQLite declared column type to a Python type
// string, using SQLite's affinity rules loosened for common declared
// names (SQLite stores any declared type text; the declaration is a
// convention). The second return value is false when declaredType had no
// known mapping and fell back to "Any". An empty declared type (allowed
// by SQLite, BLOB affinity) maps to bytes.
func MapSQLiteToPython(declaredType string) (string, bool) {
	t := strings.ToLower(strings.TrimSpace(declaredType))
	// Strip a length/precision suffix like varchar(80) or decimal(10,2).
	if i := strings.IndexByte(t, '('); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	switch {
	case t == "":
		return "bytes", true
	case t == "boolean" || t == "bool":
		return "bool", true
	case strings.Contains(t, "int"):
		return "int", true
	case t == "datetime" || t == "timestamp" || t == "date":
		return "datetime", true
	case t == "time":
		return "time", true
	case t == "numeric" || t == "decimal" || t == "money":
		return "Decimal", true
	case strings.Contains(t, "real") || strings.Contains(t, "floa") || strings.Contains(t, "doub"):
		return "float", true
	case t == "uuid":
		return "UUID", true
	case strings.Contains(t, "char") || strings.Contains(t, "clob") || strings.Contains(t, "text"):
		return "str", true
	case strings.Contains(t, "blob"):
		return "bytes", true
	default:
		return "Any", false
	}
}

// introspect lists user tables from sqlite_master and their columns via
// PRAGMA table_info, excluding excludeList and SQLite's own sqlite_*
// tables — the same contract as the other engines' Introspect. SQLite has
// no stored procedures, so only tables are emitted; Schema is always
// "main".
func introspect(db *sql.DB, excludeList []string) ([]generate.TableSchema, []string, error) {
	excludeSet := make(map[string]bool)
	for _, e := range excludeList {
		excludeSet[strings.ToLower(strings.TrimSpace(e))] = true
	}

	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, nil, fmt.Errorf("introspecting schema: %w", err)
	}
	defer rows.Close()

	var tableNames []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, nil, fmt.Errorf("scanning table name: %w", err)
		}
		if excludeSet[strings.ToLower(name)] || excludeSet[strings.ToLower(DefaultSchema+"."+name)] {
			continue
		}
		tableNames = append(tableNames, name)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterating table rows: %w", err)
	}

	var result []generate.TableSchema
	var unmapped []string
	for _, name := range tableNames {
		tbl := generate.TableSchema{Schema: DefaultSchema, Name: name}

		// PRAGMA table_info's argument cannot be bound; name comes from
		// sqlite_master itself (not caller input) and is double-quote
		// escaped for safety.
		colRows, err := db.Query(fmt.Sprintf(`PRAGMA table_info("%s")`, strings.ReplaceAll(name, `"`, `""`)))
		if err != nil {
			return nil, nil, fmt.Errorf("reading columns of %s: %w", name, err)
		}
		for colRows.Next() {
			var cid, notNull, pk int
			var colName, declaredType string
			var dflt sql.NullString
			if err := colRows.Scan(&cid, &colName, &declaredType, &notNull, &dflt, &pk); err != nil {
				colRows.Close()
				return nil, nil, fmt.Errorf("scanning column info of %s: %w", name, err)
			}

			pythonType, known := MapSQLiteToPython(declaredType)
			if !known {
				unmapped = append(unmapped, fmt.Sprintf("%s.%s.%s: %s", DefaultSchema, name, colName, declaredType))
			}

			tbl.Columns = append(tbl.Columns, generate.ColumnSchema{
				Name:       colName,
				PyName:     generate.SanitizeFieldName(colName),
				DataType:   declaredType,
				PythonType: pythonType,
				IsNullable: notNull == 0 && pk == 0, // INTEGER PRIMARY KEY is implicitly NOT NULL
			})
		}
		if err := colRows.Err(); err != nil {
			colRows.Close()
			return nil, nil, fmt.Errorf("iterating columns of %s: %w", name, err)
		}
		colRows.Close()
		result = append(result, tbl)
	}
	return result, unmapped, nil
}
