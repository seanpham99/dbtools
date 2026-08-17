package generate

import (
	"database/sql"
	"fmt"
	"strings"
)

type ColumnSchema struct {
	Name       string
	PyName     string // Name, sanitized for use as a Python identifier
	DataType   string
	PythonType string
	IsNullable bool
	MaxLength  sql.NullInt64
	Precision  sql.NullInt64
	Scale      sql.NullInt64
}

type TableSchema struct {
	Schema    string
	Name      string
	ClassName string
	Columns   []ColumnSchema
}

// MapMSSQLToPython maps a MSSQL DATA_TYPE to a Python type string.
// The second return value is false when dataType has no known mapping and fell back to "Any".
func MapMSSQLToPython(dataType string) (string, bool) {
	switch strings.ToLower(dataType) {
	case "bigint", "int", "smallint", "tinyint":
		return "int", true
	case "float", "real":
		return "float", true
	case "decimal", "numeric", "money", "smallmoney":
		return "Decimal", true
	case "bit":
		return "bool", true
	case "char", "varchar", "nchar", "nvarchar", "text", "ntext":
		return "str", true
	case "date", "datetime", "datetime2", "smalldatetime", "datetimeoffset":
		return "datetime", true
	case "time":
		return "time", true
	case "uniqueidentifier":
		return "UUID", true
	case "varbinary", "binary", "image":
		return "bytes", true
	default:
		return "Any", false
	}
}

// Introspect queries INFORMATION_SCHEMA for user base tables, excluding excludeList.
// The second return value lists columns whose DATA_TYPE had no known Python mapping
// and fell back to Any, formatted as "schema.table.column: data_type".
func Introspect(db *sql.DB, excludeList []string) ([]TableSchema, []string, error) {
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
		FROM INFORMATION_SCHEMA.TABLES t
		JOIN INFORMATION_SCHEMA.COLUMNS c 
			ON t.TABLE_SCHEMA = c.TABLE_SCHEMA 
			AND t.TABLE_NAME = c.TABLE_NAME
		WHERE t.TABLE_TYPE = 'BASE TABLE'
		ORDER BY c.TABLE_SCHEMA, c.TABLE_NAME, c.ORDINAL_POSITION
	`

	rows, err := db.Query(query)
	if err != nil {
		return nil, nil, fmt.Errorf("introspecting schema: %w", err)
	}
	defer rows.Close()

	tableMap := make(map[string]*TableSchema)
	var tableOrder []string
	excludedTables := make(map[string]bool)
	var unmapped []string

	for rows.Next() {
		var schemaName, tableName, colName, dataType, isNullableStr string
		var maxLen, precision, scale sql.NullInt64

		if err := rows.Scan(
			&schemaName,
			&tableName,
			&colName,
			&dataType,
			&isNullableStr,
			&maxLen,
			&precision,
			&scale,
		); err != nil {
			return nil, nil, fmt.Errorf("scanning column info: %w", err)
		}

		fullKey := fmt.Sprintf("%s.%s", schemaName, tableName)
		excluded, checked := excludedTables[fullKey]
		if !checked {
			excluded = excludeSet[strings.ToLower(tableName)] || excludeSet[strings.ToLower(fullKey)]
			excludedTables[fullKey] = excluded
		}
		if excluded {
			continue
		}

		tbl, exists := tableMap[fullKey]
		if !exists {
			tbl = &TableSchema{
				Schema: schemaName,
				Name:   tableName,
			}
			tableMap[fullKey] = tbl
			tableOrder = append(tableOrder, fullKey)
		}

		pythonType, known := MapMSSQLToPython(dataType)
		if !known {
			unmapped = append(unmapped, fmt.Sprintf("%s.%s: %s", fullKey, colName, dataType))
		}

		col := ColumnSchema{
			Name:       colName,
			PyName:     sanitizeFieldName(colName),
			DataType:   dataType,
			PythonType: pythonType,
			IsNullable: strings.ToUpper(isNullableStr) == "YES",
			MaxLength:  maxLen,
			Precision:  precision,
			Scale:      scale,
		}

		tbl.Columns = append(tbl.Columns, col)
	}

	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterating schema rows: %w", err)
	}

	result := make([]TableSchema, 0, len(tableOrder))
	for _, key := range tableOrder {
		result = append(result, *tableMap[key])
	}

	procSchemas, err := IntrospectProcs(db)
	if err != nil {
		return nil, nil, fmt.Errorf("introspecting stored procs: %w", err)
	}

	for _, p := range procSchemas {
		fullKey := fmt.Sprintf("%s.%s", p.Schema, p.Name)
		if excludedTables[fullKey] {
			continue
		}
		excluded := excludeSet[strings.ToLower(p.Name)] || excludeSet[strings.ToLower(fullKey)]
		if excluded {
			continue
		}
		result = append(result, p)
	}

	return result, unmapped, nil
}
