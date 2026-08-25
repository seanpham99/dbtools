package mysqlengine

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/seanpham99/dbtools/internal/generate"
)

// MapMySQLToPython maps a MySQL DATA_TYPE (as reported by
// information_schema.columns) to a Python type string. The second return
// value is false when dataType has no known mapping and fell back to "Any".
func MapMySQLToPython(dataType string) (string, bool) {
	switch strings.ToLower(dataType) {
	case "tinyint", "smallint", "mediumint", "int", "bigint", "year":
		return "int", true
	case "float", "double":
		return "float", true
	case "decimal", "numeric":
		return "Decimal", true
	case "char", "varchar", "text", "tinytext", "mediumtext", "longtext", "enum", "set":
		return "str", true
	case "date", "datetime", "timestamp":
		return "datetime", true
	case "time":
		return "time", true
	case "binary", "varbinary", "blob", "tinyblob", "mediumblob", "longblob":
		return "bytes", true
	case "json":
		return "Any", true
	default:
		return "Any", false
	}
}

// introspect queries information_schema for the current database's base
// tables, excluding excludeList — the same contract as the other engines'
// Introspect. MySQL has no separate schema layer (TABLE_SCHEMA = the
// current database), so unlike mssqlengine/postgresengine there is no
// cross-schema enumeration to do.
func introspect(db *sql.DB, excludeList []string) ([]generate.TableSchema, []string, error) {
	excludeSet := make(map[string]bool)
	for _, e := range excludeList {
		excludeSet[strings.ToLower(strings.TrimSpace(e))] = true
	}

	query := `
		SELECT
			c.TABLE_SCHEMA,
			c.TABLE_NAME,
			c.COLUMN_NAME,
			c.DATA_TYPE,
			c.IS_NULLABLE,
			c.CHARACTER_MAXIMUM_LENGTH,
			c.NUMERIC_PRECISION,
			c.NUMERIC_SCALE
		FROM information_schema.tables t
		JOIN information_schema.columns c
			ON t.TABLE_SCHEMA = c.TABLE_SCHEMA AND t.TABLE_NAME = c.TABLE_NAME
		WHERE t.TABLE_SCHEMA = DATABASE() AND t.TABLE_TYPE = 'BASE TABLE'
		ORDER BY c.TABLE_NAME, c.ORDINAL_POSITION
	`

	rows, err := db.Query(query)
	if err != nil {
		return nil, nil, fmt.Errorf("introspecting schema: %w", err)
	}
	defer rows.Close()

	tableMap := make(map[string]*generate.TableSchema)
	var tableOrder []string
	var unmapped []string

	for rows.Next() {
		var schemaName, tableName, colName, dataType, isNullableStr string
		var maxLen, precision, scale sql.NullInt64

		if err := rows.Scan(&schemaName, &tableName, &colName, &dataType, &isNullableStr, &maxLen, &precision, &scale); err != nil {
			return nil, nil, fmt.Errorf("scanning column info: %w", err)
		}

		if excludeSet[strings.ToLower(tableName)] {
			continue
		}

		tbl, exists := tableMap[tableName]
		if !exists {
			tbl = &generate.TableSchema{Schema: schemaName, Name: tableName}
			tableMap[tableName] = tbl
			tableOrder = append(tableOrder, tableName)
		}

		pythonType, known := MapMySQLToPython(dataType)
		if !known {
			unmapped = append(unmapped, fmt.Sprintf("%s.%s: %s", tableName, colName, dataType))
		}

		tbl.Columns = append(tbl.Columns, generate.ColumnSchema{
			Name:       colName,
			PyName:     generate.SanitizeFieldName(colName),
			DataType:   dataType,
			PythonType: pythonType,
			IsNullable: strings.ToUpper(isNullableStr) == "YES",
			MaxLength:  maxLen,
			Precision:  precision,
			Scale:      scale,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterating schema rows: %w", err)
	}

	result := make([]generate.TableSchema, 0, len(tableOrder))
	for _, name := range tableOrder {
		result = append(result, *tableMap[name])
	}
	return result, unmapped, nil
}
