package sqliteengine

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/seanpham99/dbtools/internal/generate"
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
				Name:            colName,
				PyName:          generate.SanitizeFieldName(colName),
				DataType:        declaredType,
				PythonType:      pythonType,
				IsNullable:      notNull == 0 && pk == 0, // INTEGER PRIMARY KEY is implicitly NOT NULL
				OrdinalPosition: cid + 1,
				DefaultValue:    dflt,
				IsPrimaryKey:    pk > 0,
			})
		}
		if err := colRows.Err(); err != nil {
			colRows.Close()
			return nil, nil, fmt.Errorf("iterating columns of %s: %w", name, err)
		}
		colRows.Close()

		fkRows, err := db.Query(fmt.Sprintf(`PRAGMA foreign_key_list("%s")`, strings.ReplaceAll(name, `"`, `""`)))
		if err != nil {
			return nil, nil, fmt.Errorf("reading foreign keys of %s: %w", name, err)
		}
		fkMap := make(map[int]*generate.ForeignKeySchema)
		var fkOrder []int
		for fkRows.Next() {
			var id, seq int
			var refTable, fromCol, toCol string
			var onUpdate, onDelete, match string
			if err := fkRows.Scan(&id, &seq, &refTable, &fromCol, &toCol, &onUpdate, &onDelete, &match); err != nil {
				fkRows.Close()
				return nil, nil, fmt.Errorf("scanning foreign key of %s: %w", name, err)
			}
			fk, exists := fkMap[id]
			if !exists {
				fk = &generate.ForeignKeySchema{Name: fmt.Sprintf("fk_%d", id), RefSchema: DefaultSchema, RefTable: refTable}
				fkMap[id] = fk
				fkOrder = append(fkOrder, id)
			}
			fk.Columns = append(fk.Columns, fromCol)
			fk.RefColumns = append(fk.RefColumns, toCol)
		}
		fkRows.Close()
		if err := fkRows.Err(); err != nil {
			return nil, nil, fmt.Errorf("iterating foreign keys of %s: %w", name, err)
		}
		for _, id := range fkOrder {
			tbl.ForeignKeys = append(tbl.ForeignKeys, *fkMap[id])
		}

		ixListRows, err := db.Query(fmt.Sprintf(`PRAGMA index_list("%s")`, strings.ReplaceAll(name, `"`, `""`)))
		if err != nil {
			return nil, nil, fmt.Errorf("reading indexes of %s: %w", name, err)
		}
		type ixRow struct {
			name   string
			unique bool
			origin string
		}
		var ixRows []ixRow
		for ixListRows.Next() {
			var seq int
			var idxName, origin string
			var unique, partial int
			if err := ixListRows.Scan(&seq, &idxName, &unique, &origin, &partial); err != nil {
				ixListRows.Close()
				return nil, nil, fmt.Errorf("scanning index list of %s: %w", name, err)
			}
			// origin 'pk' is the implicit PK-backing index — already
			// represented via ColumnSchema.IsPrimaryKey, excluded here.
			if origin == "pk" {
				continue
			}
			ixRows = append(ixRows, ixRow{name: idxName, unique: unique == 1, origin: origin})
		}
		ixListRows.Close()
		if err := ixListRows.Err(); err != nil {
			return nil, nil, fmt.Errorf("iterating index list of %s: %w", name, err)
		}
		for _, ix := range ixRows {
			infoRows, err := db.Query(fmt.Sprintf(`PRAGMA index_info("%s")`, strings.ReplaceAll(ix.name, `"`, `""`)))
			if err != nil {
				return nil, nil, fmt.Errorf("reading index info of %s: %w", ix.name, err)
			}
			idx := generate.IndexSchema{Name: ix.name, Unique: ix.unique}
			for infoRows.Next() {
				var seqno, cid int
				var colName string
				if err := infoRows.Scan(&seqno, &cid, &colName); err != nil {
					infoRows.Close()
					return nil, nil, fmt.Errorf("scanning index info of %s: %w", ix.name, err)
				}
				idx.Columns = append(idx.Columns, colName)
			}
			infoRows.Close()
			if err := infoRows.Err(); err != nil {
				return nil, nil, fmt.Errorf("iterating index info of %s: %w", ix.name, err)
			}
			tbl.Indexes = append(tbl.Indexes, idx)
		}

		result = append(result, tbl)
	}
	return result, unmapped, nil
}
