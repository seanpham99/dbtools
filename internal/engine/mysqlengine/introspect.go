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
			c.NUMERIC_SCALE,
			c.ORDINAL_POSITION,
			c.COLUMN_DEFAULT
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
		var ordinalPos int
		var columnDefault sql.NullString

		if err := rows.Scan(&schemaName, &tableName, &colName, &dataType, &isNullableStr, &maxLen, &precision, &scale, &ordinalPos, &columnDefault); err != nil {
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
		})
	}

	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterating schema rows: %w", err)
	}

	// Primary keys.
	pkRows, err := db.Query(`
		SELECT TABLE_NAME, COLUMN_NAME
		FROM information_schema.key_column_usage
		WHERE TABLE_SCHEMA = DATABASE() AND CONSTRAINT_NAME = 'PRIMARY'`)
	if err != nil {
		return nil, nil, fmt.Errorf("introspecting primary keys: %w", err)
	}
	pkColumns := make(map[string]map[string]bool)
	for pkRows.Next() {
		var tableName, colName string
		if err := pkRows.Scan(&tableName, &colName); err != nil {
			pkRows.Close()
			return nil, nil, fmt.Errorf("scanning primary key: %w", err)
		}
		if pkColumns[tableName] == nil {
			pkColumns[tableName] = make(map[string]bool)
		}
		pkColumns[tableName][colName] = true
	}
	pkRows.Close()
	if err := pkRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterating primary keys: %w", err)
	}
	for tableName, tbl := range tableMap {
		for i, col := range tbl.Columns {
			if pkColumns[tableName][col.Name] {
				tbl.Columns[i].IsPrimaryKey = true
			}
		}
	}

	// Foreign keys.
	fkRows, err := db.Query(`
		SELECT TABLE_NAME, CONSTRAINT_NAME, COLUMN_NAME, ORDINAL_POSITION,
			REFERENCED_TABLE_SCHEMA, REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME
		FROM information_schema.key_column_usage
		WHERE TABLE_SCHEMA = DATABASE() AND REFERENCED_TABLE_NAME IS NOT NULL
		ORDER BY TABLE_NAME, CONSTRAINT_NAME, ORDINAL_POSITION`)
	if err != nil {
		return nil, nil, fmt.Errorf("introspecting foreign keys: %w", err)
	}
	fkMap := make(map[string]map[string]*generate.ForeignKeySchema)
	fkOrder := make(map[string][]string)
	for fkRows.Next() {
		var tableName, constraintName, colName, refSchema, refTable, refColumn string
		var ordinal int
		if err := fkRows.Scan(&tableName, &constraintName, &colName, &ordinal, &refSchema, &refTable, &refColumn); err != nil {
			fkRows.Close()
			return nil, nil, fmt.Errorf("scanning foreign key: %w", err)
		}
		if fkMap[tableName] == nil {
			fkMap[tableName] = make(map[string]*generate.ForeignKeySchema)
		}
		fk, exists := fkMap[tableName][constraintName]
		if !exists {
			fk = &generate.ForeignKeySchema{Name: constraintName, RefSchema: refSchema, RefTable: refTable}
			fkMap[tableName][constraintName] = fk
			fkOrder[tableName] = append(fkOrder[tableName], constraintName)
		}
		fk.Columns = append(fk.Columns, colName)
		fk.RefColumns = append(fk.RefColumns, refColumn)
	}
	fkRows.Close()
	if err := fkRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterating foreign keys: %w", err)
	}
	for tableName, names := range fkOrder {
		tbl, ok := tableMap[tableName]
		if !ok {
			continue
		}
		for _, name := range names {
			tbl.ForeignKeys = append(tbl.ForeignKeys, *fkMap[tableName][name])
		}
	}

	// CHECK constraints (MySQL 8.0.16+).
	ckRows, err := db.Query(`
		SELECT tc.TABLE_NAME, cc.CONSTRAINT_NAME, cc.CHECK_CLAUSE
		FROM information_schema.table_constraints tc
		JOIN information_schema.check_constraints cc
			ON tc.CONSTRAINT_SCHEMA = cc.CONSTRAINT_SCHEMA AND tc.CONSTRAINT_NAME = cc.CONSTRAINT_NAME
		WHERE tc.CONSTRAINT_SCHEMA = DATABASE() AND tc.CONSTRAINT_TYPE = 'CHECK'`)
	if err != nil {
		return nil, nil, fmt.Errorf("introspecting check constraints: %w", err)
	}
	for ckRows.Next() {
		var tableName, constraintName, expr string
		if err := ckRows.Scan(&tableName, &constraintName, &expr); err != nil {
			ckRows.Close()
			return nil, nil, fmt.Errorf("scanning check constraint: %w", err)
		}
		if tbl, ok := tableMap[tableName]; ok {
			tbl.CheckConstraints = append(tbl.CheckConstraints, generate.CheckConstraintSchema{Name: constraintName, Expression: expr})
		}
	}
	ckRows.Close()
	if err := ckRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterating check constraints: %w", err)
	}

	// Indexes — excludes PRIMARY (MySQL's reserved name for the PK-backing index).
	ixRows, err := db.Query(`
		SELECT TABLE_NAME, INDEX_NAME, COLUMN_NAME, SEQ_IN_INDEX, NON_UNIQUE
		FROM information_schema.statistics
		WHERE TABLE_SCHEMA = DATABASE() AND INDEX_NAME != 'PRIMARY'
		ORDER BY TABLE_NAME, INDEX_NAME, SEQ_IN_INDEX`)
	if err != nil {
		return nil, nil, fmt.Errorf("introspecting indexes: %w", err)
	}
	ixMap := make(map[string]map[string]*generate.IndexSchema)
	ixOrder := make(map[string][]string)
	for ixRows.Next() {
		var tableName, indexName, colName string
		var seq, nonUnique int
		if err := ixRows.Scan(&tableName, &indexName, &colName, &seq, &nonUnique); err != nil {
			ixRows.Close()
			return nil, nil, fmt.Errorf("scanning index: %w", err)
		}
		if ixMap[tableName] == nil {
			ixMap[tableName] = make(map[string]*generate.IndexSchema)
		}
		idx, exists := ixMap[tableName][indexName]
		if !exists {
			idx = &generate.IndexSchema{Name: indexName, Unique: nonUnique == 0}
			ixMap[tableName][indexName] = idx
			ixOrder[tableName] = append(ixOrder[tableName], indexName)
		}
		idx.Columns = append(idx.Columns, colName)
	}
	ixRows.Close()
	if err := ixRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterating indexes: %w", err)
	}
	for tableName, names := range ixOrder {
		tbl, ok := tableMap[tableName]
		if !ok {
			continue
		}
		for _, name := range names {
			tbl.Indexes = append(tbl.Indexes, *ixMap[tableName][name])
		}
	}

	result := make([]generate.TableSchema, 0, len(tableOrder))
	for _, name := range tableOrder {
		result = append(result, *tableMap[name])
	}
	return result, unmapped, nil
}
