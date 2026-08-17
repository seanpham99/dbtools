package postgresengine

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/dbtools/dbtools/internal/generate"
)

// MapPostgresToPython maps a Postgres data_type (as reported by
// information_schema.columns) to a Python type string. The second return
// value is false when dataType has no known mapping and fell back to "Any".
func MapPostgresToPython(dataType string) (string, bool) {
	switch strings.ToLower(dataType) {
	case "smallint", "integer", "bigint", "int2", "int4", "int8", "smallserial", "serial", "bigserial":
		return "int", true
	case "real", "double precision", "float4", "float8":
		return "float", true
	case "numeric", "decimal", "money":
		return "Decimal", true
	case "boolean", "bool":
		return "bool", true
	case "character", "character varying", "char", "varchar", "text", "citext", "name":
		return "str", true
	case "date", "timestamp", "timestamp without time zone", "timestamp with time zone", "timestamptz":
		return "datetime", true
	case "time", "time without time zone", "time with time zone", "timetz":
		return "time", true
	case "uuid":
		return "UUID", true
	case "bytea":
		return "bytes", true
	case "json", "jsonb":
		// Deliberately Any without a warning: arbitrary JSON has no
		// tighter faithful Python type.
		return "Any", true
	default:
		return "Any", false
	}
}

// introspect queries information_schema for user base tables (skipping
// Postgres's own catalogs), excluding excludeList — the same contract as
// the MSSQL engine's Introspect. Postgres has no MSSQL-style stored-proc
// contract convention, so only tables are emitted.
func introspect(db *sql.DB, excludeList []string) ([]generate.TableSchema, []string, error) {
	excludeSet := make(map[string]bool)
	for _, e := range excludeList {
		excludeSet[strings.ToLower(strings.TrimSpace(e))] = true
	}

	query := `
		SELECT
			c.table_schema,
			c.table_name,
			c.column_name,
			c.data_type,
			c.is_nullable,
			c.character_maximum_length,
			c.numeric_precision,
			c.numeric_scale
		FROM information_schema.tables t
		JOIN information_schema.columns c
			ON t.table_schema = c.table_schema
			AND t.table_name = c.table_name
		WHERE t.table_type = 'BASE TABLE'
			AND t.table_schema NOT IN ('pg_catalog', 'information_schema')
		ORDER BY c.table_schema, c.table_name, c.ordinal_position
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

		fullKey := fmt.Sprintf("%s.%s", schemaName, tableName)
		if excludeSet[strings.ToLower(tableName)] || excludeSet[strings.ToLower(fullKey)] {
			continue
		}

		tbl, exists := tableMap[fullKey]
		if !exists {
			tbl = &generate.TableSchema{Schema: schemaName, Name: tableName}
			tableMap[fullKey] = tbl
			tableOrder = append(tableOrder, fullKey)
		}

		pythonType, known := MapPostgresToPython(dataType)
		if !known {
			unmapped = append(unmapped, fmt.Sprintf("%s.%s: %s", fullKey, colName, dataType))
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
	for _, key := range tableOrder {
		result = append(result, *tableMap[key])
	}
	return result, unmapped, nil
}
