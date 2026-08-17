package generate

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

// ProcParam is one field of a stored procedure's OPENJSON(@json_payload) contract.
type ProcParam struct {
	JSONPath   string // e.g. "$.trade_date"
	Name       string // field name, the JSON path's leaf segment
	SQLType    string // TRY_CONVERT'd/CAST type if converted, else declared type or NVARCHAR
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

// findBalancedParenEnd returns the index of the ')' that closes the '(' at s[openIdx].
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
		// Check that the CAST open paren has not been closed yet before matchStart
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

// ExtractOpenJSONContract parses a stored procedure's body for its
// OPENJSON(@param) WITH (...) contract or JSON_VALUE idiom, cross-referencing
// TRY_CONVERT / CAST calls for real types. Returns nil if no OPENJSON call is present.
func ExtractOpenJSONContract(procBody string) []ProcParam {
	convertedTypes := make(map[string]string)
	for _, m := range tryConvertRe.FindAllStringSubmatch(procBody, -1) {
		convertedTypes[strings.ToLower(m[2])] = m[1]
	}

	// 1. Try OPENJSON WITH (...) blocks (keeping the block with most columns for Gap #1)
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

	// 2. Fallback for Gap #2: OPENJSON without WITH clause, using JSON_VALUE(j.value, '$.path')
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

// IntrospectProcs finds every stored procedure whose body calls
// OPENJSON(@json_payload) WITH (...) or OPENJSON(@json_payload) AS j
// and returns its contract as a TableSchema — reusing the same rendering
// machinery as table introspection.
func IntrospectProcs(db *sql.DB) ([]TableSchema, error) {
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

	var result []TableSchema
	for rows.Next() {
		var schemaName, procName, definition string
		if err := rows.Scan(&schemaName, &procName, &definition); err != nil {
			return nil, fmt.Errorf("scanning proc definition: %w", err)
		}

		params := ExtractOpenJSONContract(definition)
		if params == nil {
			continue
		}

		cols := make([]ColumnSchema, len(params))
		for i, p := range params {
			cols[i] = ColumnSchema{
				Name:       p.Name,
				PyName:     SanitizeFieldName(p.Name),
				DataType:   p.SQLType,
				PythonType: p.PythonType,
				IsNullable: true,
			}
		}
		result = append(result, TableSchema{
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
