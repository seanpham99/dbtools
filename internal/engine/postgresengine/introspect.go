package postgresengine

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/seanpham99/dbtools/internal/generate"
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
			c.numeric_scale,
			c.ordinal_position,
			c.column_default
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
		var ordinalPos int
		var columnDefault sql.NullString

		if err := rows.Scan(&schemaName, &tableName, &colName, &dataType, &isNullableStr, &maxLen, &precision, &scale, &ordinalPos, &columnDefault); err != nil {
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
		SELECT tc.table_schema, tc.table_name, kcu.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
			ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
		WHERE tc.constraint_type = 'PRIMARY KEY'`)
	if err != nil {
		return nil, nil, fmt.Errorf("introspecting primary keys: %w", err)
	}
	pkColumns := make(map[string]map[string]bool) // fullKey -> column name -> true
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
		SELECT tc.table_schema, tc.table_name, tc.constraint_name, kcu.column_name, kcu.ordinal_position,
			ukcu.table_schema, ukcu.table_name, ukcu.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
			ON tc.constraint_catalog = kcu.constraint_catalog
			AND tc.constraint_schema = kcu.constraint_schema
			AND tc.constraint_name = kcu.constraint_name
		JOIN information_schema.referential_constraints rc
			ON tc.constraint_catalog = rc.constraint_catalog
			AND tc.constraint_schema = rc.constraint_schema
			AND tc.constraint_name = rc.constraint_name
		JOIN information_schema.key_column_usage ukcu
			ON rc.unique_constraint_catalog = ukcu.constraint_catalog
			AND rc.unique_constraint_schema = ukcu.constraint_schema
			AND rc.unique_constraint_name = ukcu.constraint_name
			AND kcu.position_in_unique_constraint = ukcu.ordinal_position
		WHERE tc.constraint_type = 'FOREIGN KEY'
		ORDER BY tc.table_schema, tc.table_name, tc.constraint_name, kcu.ordinal_position`)
	if err != nil {
		return nil, nil, fmt.Errorf("introspecting foreign keys: %w", err)
	}
	fkMap := make(map[string]map[string]*generate.ForeignKeySchema) // fullKey -> constraint name -> FK
	var fkOrder = make(map[string][]string)
	for fkRows.Next() {
		var schemaName, tableName, constraintName, colName, refSchema, refTable, refColumn string
		var ordinal int
		if err := fkRows.Scan(&schemaName, &tableName, &constraintName, &colName, &ordinal, &refSchema, &refTable, &refColumn); err != nil {
			fkRows.Close()
			return nil, nil, fmt.Errorf("scanning foreign key: %w", err)
		}
		fullKey := fmt.Sprintf("%s.%s", schemaName, tableName)
		if fkMap[fullKey] == nil {
			fkMap[fullKey] = make(map[string]*generate.ForeignKeySchema)
		}
		fk, exists := fkMap[fullKey][constraintName]
		if !exists {
			fk = &generate.ForeignKeySchema{Name: constraintName, RefSchema: refSchema, RefTable: refTable}
			fkMap[fullKey][constraintName] = fk
			fkOrder[fullKey] = append(fkOrder[fullKey], constraintName)
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
		SELECT tc.table_schema, tc.table_name, tc.constraint_name, cc.check_clause
		FROM information_schema.table_constraints tc
		JOIN information_schema.check_constraints cc
			ON tc.constraint_schema = cc.constraint_schema AND tc.constraint_name = cc.constraint_name
		WHERE tc.constraint_type = 'CHECK'
			AND tc.table_schema NOT IN ('pg_catalog', 'information_schema')
			AND cc.check_clause NOT LIKE '%IS NOT NULL'`)
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

	// Indexes — excludes the primary key's backing index (already
	// represented via ColumnSchema.IsPrimaryKey) and limits indkey to
	// indnkeyatts so INCLUDE columns are omitted.
	ixRows, err := db.Query(`
		SELECT n.nspname, t.relname, i.relname, a.attname, ix.indisunique,
			key.ordinality
		FROM pg_index ix
		JOIN pg_class i ON i.oid = ix.indexrelid
		JOIN pg_class t ON t.oid = ix.indrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		JOIN LATERAL unnest(ix.indkey) WITH ORDINALITY AS key(attnum, ordinality)
			ON key.ordinality <= ix.indnkeyatts
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = key.attnum
		WHERE t.relkind = 'r' AND n.nspname NOT IN ('pg_catalog', 'information_schema')
			AND NOT ix.indisprimary
		ORDER BY n.nspname, t.relname, i.relname, key.ordinality`)
	if err != nil {
		return nil, nil, fmt.Errorf("introspecting indexes: %w", err)
	}
	ixMap := make(map[string]map[string]*generate.IndexSchema)
	ixOrder := make(map[string][]string)
	for ixRows.Next() {
		var schemaName, tableName, indexName, colName string
		var unique bool
		var pos int
		if err := ixRows.Scan(&schemaName, &tableName, &indexName, &colName, &unique, &pos); err != nil {
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
	return result, unmapped, nil
}
