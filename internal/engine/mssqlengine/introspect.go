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
			c.NUMERIC_SCALE,
			c.ORDINAL_POSITION,
			c.COLUMN_DEFAULT
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
		var ordinalPos int
		var columnDefault sql.NullString

		if err := rows.Scan(
			&schemaName,
			&tableName,
			&colName,
			&dataType,
			&isNullableStr,
			&maxLen,
			&precision,
			&scale,
			&ordinalPos,
			&columnDefault,
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
			Name:            colName,
			PyName:          generate.SanitizeFieldName(colName),
			DataType:        dataType,
			PythonType:      pythonType,
			IsNullable:      strings.ToUpper(isNullableStr) == "YES",
			MaxLength:       maxLen,
			Precision:       precision,
			Scale:           scale,
			OrdinalPosition: ordinalPos,
			DefaultValue:    columnDefault,
		}

		tbl.Columns = append(tbl.Columns, col)
	}

	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterating schema rows: %w", err)
	}

	// Primary keys, via sys.indexes/sys.index_columns (is_primary_key=1).
	pkRows, err := db.Query(`
		SELECT s.name, t.name, c.name
		FROM sys.indexes i
		JOIN sys.tables t ON t.object_id = i.object_id
		JOIN sys.schemas s ON s.schema_id = t.schema_id
		JOIN sys.index_columns ic ON ic.object_id = i.object_id AND ic.index_id = i.index_id
		JOIN sys.columns c ON c.object_id = ic.object_id AND c.column_id = ic.column_id
		WHERE i.is_primary_key = 1`)
	if err != nil {
		return nil, nil, fmt.Errorf("introspecting primary keys: %w", err)
	}
	pkColumns := make(map[string]map[string]bool)
	for pkRows.Next() {
		var schemaName, tableName, colName string
		if err := pkRows.Scan(&schemaName, &tableName, &colName); err != nil {
			pkRows.Close()
			return nil, nil, fmt.Errorf("scanning primary key: %w", err)
		}
		fullKey := fmt.Sprintf("%s.%s", schemaName, tableName)
		if pkColumns[fullKey] == nil {
			pkColumns[fullKey] = make(map[string]bool)
		}
		pkColumns[fullKey][colName] = true
	}
	pkRows.Close()
	if err := pkRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterating primary keys: %w", err)
	}
	for fullKey, tbl := range tableMap {
		for i, col := range tbl.Columns {
			if pkColumns[fullKey][col.Name] {
				tbl.Columns[i].IsPrimaryKey = true
			}
		}
	}

	// Foreign keys.
	fkRows, err := db.Query(`
		SELECT s.name, t.name, fk.name, c.name, fkc.constraint_column_id,
			rs.name, rt.name, rc.name
		FROM sys.foreign_keys fk
		JOIN sys.tables t ON t.object_id = fk.parent_object_id
		JOIN sys.schemas s ON s.schema_id = t.schema_id
		JOIN sys.foreign_key_columns fkc ON fkc.constraint_object_id = fk.object_id
		JOIN sys.columns c ON c.object_id = fkc.parent_object_id AND c.column_id = fkc.parent_column_id
		JOIN sys.tables rt ON rt.object_id = fk.referenced_object_id
		JOIN sys.schemas rs ON rs.schema_id = rt.schema_id
		JOIN sys.columns rc ON rc.object_id = fkc.referenced_object_id AND rc.column_id = fkc.referenced_column_id
		ORDER BY s.name, t.name, fk.name, fkc.constraint_column_id`)
	if err != nil {
		return nil, nil, fmt.Errorf("introspecting foreign keys: %w", err)
	}
	fkMap := make(map[string]map[string]*generate.ForeignKeySchema)
	fkOrder := make(map[string][]string)
	for fkRows.Next() {
		var schemaName, tableName, fkName, colName, refSchema, refTable, refColumn string
		var colID int
		if err := fkRows.Scan(&schemaName, &tableName, &fkName, &colName, &colID, &refSchema, &refTable, &refColumn); err != nil {
			fkRows.Close()
			return nil, nil, fmt.Errorf("scanning foreign key: %w", err)
		}
		fullKey := fmt.Sprintf("%s.%s", schemaName, tableName)
		if fkMap[fullKey] == nil {
			fkMap[fullKey] = make(map[string]*generate.ForeignKeySchema)
		}
		fk, exists := fkMap[fullKey][fkName]
		if !exists {
			fk = &generate.ForeignKeySchema{Name: fkName, RefSchema: refSchema, RefTable: refTable}
			fkMap[fullKey][fkName] = fk
			fkOrder[fullKey] = append(fkOrder[fullKey], fkName)
		}
		fk.Columns = append(fk.Columns, colName)
		fk.RefColumns = append(fk.RefColumns, refColumn)
	}
	fkRows.Close()
	if err := fkRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterating foreign keys: %w", err)
	}
	for fullKey, names := range fkOrder {
		tbl, ok := tableMap[fullKey]
		if !ok {
			continue
		}
		for _, name := range names {
			tbl.ForeignKeys = append(tbl.ForeignKeys, *fkMap[fullKey][name])
		}
	}

	// CHECK constraints.
	ckRows, err := db.Query(`
		SELECT s.name, t.name, cc.name, cc.definition
		FROM sys.check_constraints cc
		JOIN sys.tables t ON t.object_id = cc.parent_object_id
		JOIN sys.schemas s ON s.schema_id = t.schema_id`)
	if err != nil {
		return nil, nil, fmt.Errorf("introspecting check constraints: %w", err)
	}
	for ckRows.Next() {
		var schemaName, tableName, constraintName, expr string
		if err := ckRows.Scan(&schemaName, &tableName, &constraintName, &expr); err != nil {
			ckRows.Close()
			return nil, nil, fmt.Errorf("scanning check constraint: %w", err)
		}
		fullKey := fmt.Sprintf("%s.%s", schemaName, tableName)
		if tbl, ok := tableMap[fullKey]; ok {
			tbl.CheckConstraints = append(tbl.CheckConstraints, generate.CheckConstraintSchema{Name: constraintName, Expression: expr})
		}
	}
	ckRows.Close()
	if err := ckRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterating check constraints: %w", err)
	}

	// Indexes — excludes the PK-backing index and INCLUDE columns.
	ixRows, err := db.Query(`
		SELECT s.name, t.name, i.name, c.name, i.is_unique, ic.key_ordinal
		FROM sys.indexes i
		JOIN sys.tables t ON t.object_id = i.object_id
		JOIN sys.schemas s ON s.schema_id = t.schema_id
		JOIN sys.index_columns ic ON ic.object_id = i.object_id AND ic.index_id = i.index_id
		JOIN sys.columns c ON c.object_id = ic.object_id AND c.column_id = ic.column_id
		WHERE i.name IS NOT NULL AND i.is_primary_key = 0 AND ic.is_included_column = 0
		ORDER BY s.name, t.name, i.name, ic.key_ordinal`)
	if err != nil {
		return nil, nil, fmt.Errorf("introspecting indexes: %w", err)
	}
	ixMap := make(map[string]map[string]*generate.IndexSchema)
	ixOrder := make(map[string][]string)
	for ixRows.Next() {
		var schemaName, tableName, indexName, colName string
		var unique bool
		var ordinal int
		if err := ixRows.Scan(&schemaName, &tableName, &indexName, &colName, &unique, &ordinal); err != nil {
			ixRows.Close()
			return nil, nil, fmt.Errorf("scanning index: %w", err)
		}
		fullKey := fmt.Sprintf("%s.%s", schemaName, tableName)
		if ixMap[fullKey] == nil {
			ixMap[fullKey] = make(map[string]*generate.IndexSchema)
		}
		idx, exists := ixMap[fullKey][indexName]
		if !exists {
			idx = &generate.IndexSchema{Name: indexName, Unique: unique}
			ixMap[fullKey][indexName] = idx
			ixOrder[fullKey] = append(ixOrder[fullKey], indexName)
		}
		idx.Columns = append(idx.Columns, colName)
	}
	ixRows.Close()
	if err := ixRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterating indexes: %w", err)
	}
	for fullKey, names := range ixOrder {
		tbl, ok := tableMap[fullKey]
		if !ok {
			continue
		}
		for _, name := range names {
			tbl.Indexes = append(tbl.Indexes, *ixMap[fullKey][name])
		}
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
