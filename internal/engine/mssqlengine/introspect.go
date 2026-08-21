package mssqlengine

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"github.com/seanpham99/dbtools/internal/generate"
)

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
func Introspect(db *sql.DB, excludeList []string) ([]generate.TableSchema, []string, error) {
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

	tableMap := make(map[string]*generate.TableSchema)
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
			tbl = &generate.TableSchema{
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

		col := generate.ColumnSchema{
			Name:       colName,
			PyName:     generate.SanitizeFieldName(colName),
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

	result := make([]generate.TableSchema, 0, len(tableOrder))
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

// ProcParam is one field of a stored procedure's OPENJSON(@json_payload) contract.
type ProcParam struct {
	JSONPath   string
	Name       string
	SQLType    string
	PythonType string
}

var (
	openJSONWithRe      = regexp.MustCompile(`(?is)OPENJSON\s*\(\s*@\w+\s*\)\s*WITH\s*\(`)
	withColumnRe        = regexp.MustCompile(`(?im)^\s*\[?([A-Za-z_][A-Za-z0-9_.]*)\]?\s+([A-Za-z0-9]+(?:\(\s*\d+(?:\s*,\s*\d+)?\s*\))?)(?:\s+'(\$\.[A-Za-z0-9_.\[\]]+)')?`)
	tryConvertRe        = regexp.MustCompile(`(?i)TRY_CONVERT\s*\(\s*([A-Za-z]+(?:\(\s*\d+(?:\s*,\s*\d+)?\s*\))?)\s*,\s*\[?([A-Za-z_][A-Za-z0-9_.]*)\]?\s*\)`)
	jsonValueRe         = regexp.MustCompile(`(?i)JSON_VALUE\s*\(\s*(?:[A-Za-z_][A-Za-z0-9_.]*)\s*,\s*'(\$\.[A-Za-z0-9_.\[\]]+)'\s*\)`)
	tryConvertSnippetRe = regexp.MustCompile(`(?i)TRY_CONVERT\s*\(\s*([A-Za-z]+(?:\(\s*\d+(?:\s*,\s*\d+)?\s*\))?)\s*,`)
	asTypePostRe        = regexp.MustCompile(`(?i)\bAS\s+([A-Za-z]+(?:\(\s*\d+(?:\s*,\s*\d+)?\s*\))?)`)
)

var knownSQLTypes = map[string]bool{
	"BIGINT": true, "INT": true, "SMALLINT": true, "TINYINT": true,
	"BIT": true, "DECIMAL": true, "NUMERIC": true, "FLOAT": true, "REAL": true,
	"MONEY": true, "SMALLMONEY": true, "DATE": true, "DATETIME": true,
	"DATETIME2": true, "SMALLDATETIME": true, "DATETIMEOFFSET": true, "TIME": true,
	"NVARCHAR": true, "VARCHAR": true, "NCHAR": true, "CHAR": true,
	"VARBINARY": true, "BINARY": true, "UNIQUEIDENTIFIER": true,
}

func findBalancedParenEnd(s string, openIdx int) int {
	depth := 0
	for i := openIdx; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func baseSQLType(t string) string {
	if idx := strings.IndexByte(t, '('); idx != -1 {
		t = t[:idx]
	}
	return strings.TrimSpace(t)
}

func extractJSONValueSQLType(procBody string, matchStart, matchEnd int) string {
	start := matchStart - 150
	if start < 0 {
		start = 0
	}
	pre := procBody[start:matchStart]

	end := matchEnd + 150
	if end > len(procBody) {
		end = len(procBody)
	}
	post := procBody[matchEnd:end]

	if m := tryConvertSnippetRe.FindStringSubmatch(pre); m != nil {
		t := baseSQLType(m[1])
		if knownSQLTypes[strings.ToUpper(t)] {
			return t
		}
	}

	lastCastIdx := strings.LastIndex(strings.ToUpper(pre), "CAST(")
	if lastCastIdx != -1 {
		between := pre[lastCastIdx:]
		depth := 0
		for i := 0; i < len(between); i++ {
			if between[i] == '(' {
				depth++
			} else if between[i] == ')' {
				depth--
			}
		}
		if depth > 0 {
			if m := asTypePostRe.FindStringSubmatch(post); m != nil {
				t := baseSQLType(m[1])
				if knownSQLTypes[strings.ToUpper(t)] {
					return t
				}
			}
		}
	}

	directAsRe := regexp.MustCompile(`(?i)^\s*\)*\s+AS\s+([A-Za-z]+(?:\(\s*\d+(?:\s*,\s*\d+)?\s*\))?)`)
	if m := directAsRe.FindStringSubmatch(post); m != nil {
		t := baseSQLType(m[1])
		if knownSQLTypes[strings.ToUpper(t)] {
			return t
		}
	}

	return "NVARCHAR"
}

// ExtractOpenJSONContract parses a stored procedure's body for its OPENJSON contract.
func ExtractOpenJSONContract(procBody string) []ProcParam {
	convertedTypes := make(map[string]string)
	for _, m := range tryConvertRe.FindAllStringSubmatch(procBody, -1) {
		convertedTypes[strings.ToLower(m[2])] = m[1]
	}

	locs := openJSONWithRe.FindAllStringIndex(procBody, -1)
	var bestParams []ProcParam

	for _, loc := range locs {
		openParenIdx := loc[1] - 1
		closeIdx := findBalancedParenEnd(procBody, openParenIdx)
		if closeIdx == -1 {
			continue
		}
		withBlock := procBody[openParenIdx+1 : closeIdx]

		var params []ProcParam
		seen := make(map[string]bool)
		for _, m := range withColumnRe.FindAllStringSubmatch(withBlock, -1) {
			colName, declaredType := m[1], m[2]
			jsonPath := m[3]
			if jsonPath == "" {
				jsonPath = "$." + colName
			}
			fieldName := jsonPath[strings.LastIndex(jsonPath, ".")+1:]
			if seen[strings.ToLower(fieldName)] {
				continue
			}
			seen[strings.ToLower(fieldName)] = true

			sqlType := baseSQLType(declaredType)
			if converted, ok := convertedTypes[strings.ToLower(colName)]; ok {
				sqlType = baseSQLType(converted)
			}

			pythonType, _ := MapMSSQLToPython(sqlType)
			params = append(params, ProcParam{
				JSONPath:   jsonPath,
				Name:       fieldName,
				SQLType:    sqlType,
				PythonType: pythonType,
			})
		}

		if len(params) > len(bestParams) {
			bestParams = params
		}
	}

	if len(bestParams) > 0 {
		return bestParams
	}

	if !strings.Contains(strings.ToUpper(procBody), "OPENJSON") {
		return nil
	}

	matches := jsonValueRe.FindAllStringIndex(procBody, -1)
	if len(matches) == 0 {
		return nil
	}

	var params []ProcParam
	seen := make(map[string]bool)

	for _, loc := range matches {
		matchStr := procBody[loc[0]:loc[1]]
		sub := jsonValueRe.FindStringSubmatch(matchStr)
		if sub == nil {
			continue
		}
		jsonPath := sub[1]
		fieldName := jsonPath[strings.LastIndex(jsonPath, ".")+1:]
		if seen[strings.ToLower(fieldName)] {
			continue
		}
		seen[strings.ToLower(fieldName)] = true

		sqlType := extractJSONValueSQLType(procBody, loc[0], loc[1])
		pythonType, _ := MapMSSQLToPython(sqlType)

		params = append(params, ProcParam{
			JSONPath:   jsonPath,
			Name:       fieldName,
			SQLType:    sqlType,
			PythonType: pythonType,
		})
	}

	if len(params) == 0 {
		return nil
	}
	return params
}

// IntrospectProcs finds stored procedures and returns their OPENJSON contracts.
func IntrospectProcs(db *sql.DB) ([]generate.TableSchema, error) {
	rows, err := db.Query(`
		SELECT
			OBJECT_SCHEMA_NAME(m.object_id),
			OBJECT_NAME(m.object_id),
			m.definition
		FROM sys.sql_modules m
		JOIN sys.objects o ON o.object_id = m.object_id
		WHERE o.type = 'P'
		ORDER BY OBJECT_SCHEMA_NAME(m.object_id), OBJECT_NAME(m.object_id)
	`)
	if err != nil {
		return nil, fmt.Errorf("querying sys.sql_modules: %w", err)
	}
	defer rows.Close()

	var result []generate.TableSchema
	for rows.Next() {
		var schemaName, procName, definition string
		if err := rows.Scan(&schemaName, &procName, &definition); err != nil {
			return nil, fmt.Errorf("scanning proc definition: %w", err)
		}

		params := ExtractOpenJSONContract(definition)
		if params == nil {
			continue
		}

		cols := make([]generate.ColumnSchema, len(params))
		for i, p := range params {
			cols[i] = generate.ColumnSchema{
				Name:       p.Name,
				PyName:     generate.SanitizeFieldName(p.Name),
				DataType:   p.SQLType,
				PythonType: p.PythonType,
				IsNullable: true,
			}
		}
		result = append(result, generate.TableSchema{
			Schema:  schemaName,
			Name:    strings.TrimPrefix(procName, "usp_") + "_payload",
			Columns: cols,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating proc rows: %w", err)
	}
	return result, nil
}
