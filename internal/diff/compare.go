package diff

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/seanpham99/dbtools/internal/generate"
)

type Kind string

const (
	KindMissing Kind = "MISSING" // in scratch (migrations), not in target
	KindExtra   Kind = "EXTRA"   // in target, not in scratch (migrations)
	KindChanged Kind = "CHANGED"
)

type ObjectType string

const (
	ObjectTable      ObjectType = "table"
	ObjectColumn     ObjectType = "column"
	ObjectIndex      ObjectType = "index"
	ObjectForeignKey ObjectType = "foreign_key"
	ObjectCheck      ObjectType = "check_constraint"
)

// Finding is one structural difference between the scratch (migrations)
// and target (live) databases.
type Finding struct {
	Kind   Kind       `json:"kind"`
	Object ObjectType `json:"object"`
	Table  string     `json:"table"`  // schema-qualified, e.g. "public.orders"
	Name   string     `json:"name"`   // column/index/FK/check name; "" for a table-level finding
	Detail string     `json:"detail"` // human-readable specifics, e.g. "type text vs varchar(50)"
}

func tableIdent(schema, name string) string {
	if schema != "" {
		return fmt.Sprintf("%s.%s", schema, name)
	}
	return name
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func nullInt64Str(n sql.NullInt64) string {
	if n.Valid {
		return fmt.Sprintf("%d", n.Int64)
	}
	return "<none>"
}

// Compare returns every structural difference between scratch (the
// migrations' truth) and target (the live database), plus a separate
// slice of purely informational notes (currently: column ordinal-position
// differences) that are never treated as drift.
func Compare(scratch, target []generate.TableSchema) (findings []Finding, notes []string) {
	scratchMap := make(map[string]generate.TableSchema, len(scratch))
	for _, t := range scratch {
		scratchMap[tableIdent(t.Schema, t.Name)] = t
	}

	targetMap := make(map[string]generate.TableSchema, len(target))
	for _, t := range target {
		targetMap[tableIdent(t.Schema, t.Name)] = t
	}

	// Union of all table keys, sorted for deterministic output.
	allTableKeys := make(map[string]struct{})
	for k := range scratchMap {
		allTableKeys[k] = struct{}{}
	}
	for k := range targetMap {
		allTableKeys[k] = struct{}{}
	}
	tableNames := make([]string, 0, len(allTableKeys))
	for k := range allTableKeys {
		tableNames = append(tableNames, k)
	}
	sort.Strings(tableNames)

	for _, tblKey := range tableNames {
		sTbl, inScratch := scratchMap[tblKey]
		tTbl, inTarget := targetMap[tblKey]

		switch {
		case inScratch && !inTarget:
			findings = append(findings, Finding{
				Kind:   KindMissing,
				Object: ObjectTable,
				Table:  tblKey,
				Name:   "",
				Detail: "table exists in migrations but missing in target",
			})
		case !inScratch && inTarget:
			findings = append(findings, Finding{
				Kind:   KindExtra,
				Object: ObjectTable,
				Table:  tblKey,
				Name:   "",
				Detail: "table exists in target but missing in migrations",
			})
		case inScratch && inTarget:
			tblFindings, tblNotes := compareTable(tblKey, sTbl, tTbl)
			findings = append(findings, tblFindings...)
			notes = append(notes, tblNotes...)
		}
	}

	return findings, notes
}

func compareTable(tblKey string, sTbl, tTbl generate.TableSchema) ([]Finding, []string) {
	var findings []Finding
	var notes []string

	// 1. Columns
	sCols := make(map[string]generate.ColumnSchema, len(sTbl.Columns))
	for _, c := range sTbl.Columns {
		sCols[c.Name] = c
	}
	tCols := make(map[string]generate.ColumnSchema, len(tTbl.Columns))
	for _, c := range tTbl.Columns {
		tCols[c.Name] = c
	}

	allColNamesMap := make(map[string]struct{})
	for k := range sCols {
		allColNamesMap[k] = struct{}{}
	}
	for k := range tCols {
		allColNamesMap[k] = struct{}{}
	}
	var colNames []string
	// Maintain scratch column order first, then any extra columns from target
	for _, c := range sTbl.Columns {
		colNames = append(colNames, c.Name)
	}
	for _, c := range tTbl.Columns {
		if _, ok := sCols[c.Name]; !ok {
			colNames = append(colNames, c.Name)
		}
	}

	hasColDiff := false
	ordinalDiff := false

	for _, colName := range colNames {
		sCol, inScratch := sCols[colName]
		tCol, inTarget := tCols[colName]

		switch {
		case inScratch && !inTarget:
			hasColDiff = true
			findings = append(findings, Finding{
				Kind:   KindMissing,
				Object: ObjectColumn,
				Table:  tblKey,
				Name:   colName,
				Detail: "column exists in migrations but missing in target",
			})
		case !inScratch && inTarget:
			hasColDiff = true
			findings = append(findings, Finding{
				Kind:   KindExtra,
				Object: ObjectColumn,
				Table:  tblKey,
				Name:   colName,
				Detail: "column exists in target but missing in migrations",
			})
		case inScratch && inTarget:
			var diffs []string
			if sCol.DataType != tCol.DataType {
				diffs = append(diffs, fmt.Sprintf("data type %s vs %s", sCol.DataType, tCol.DataType))
			}
			if sCol.IsNullable != tCol.IsNullable {
				diffs = append(diffs, fmt.Sprintf("nullable %t vs %t", sCol.IsNullable, tCol.IsNullable))
			}
			if sCol.MaxLength != tCol.MaxLength {
				diffs = append(diffs, fmt.Sprintf("max length %s vs %s", nullInt64Str(sCol.MaxLength), nullInt64Str(tCol.MaxLength)))
			}
			if sCol.Precision != tCol.Precision {
				diffs = append(diffs, fmt.Sprintf("precision %s vs %s", nullInt64Str(sCol.Precision), nullInt64Str(tCol.Precision)))
			}
			if sCol.Scale != tCol.Scale {
				diffs = append(diffs, fmt.Sprintf("scale %s vs %s", nullInt64Str(sCol.Scale), nullInt64Str(tCol.Scale)))
			}
			if sCol.DefaultValue.Valid != tCol.DefaultValue.Valid || sCol.DefaultValue.String != tCol.DefaultValue.String {
				diffs = append(diffs, fmt.Sprintf("default %q vs %q", sCol.DefaultValue.String, tCol.DefaultValue.String))
			}
			if sCol.IsPrimaryKey != tCol.IsPrimaryKey {
				diffs = append(diffs, fmt.Sprintf("primary key %t vs %t", sCol.IsPrimaryKey, tCol.IsPrimaryKey))
			}

			if len(diffs) > 0 {
				hasColDiff = true
				findings = append(findings, Finding{
					Kind:   KindChanged,
					Object: ObjectColumn,
					Table:  tblKey,
					Name:   colName,
					Detail: strings.Join(diffs, "; "),
				})
			}

			if sCol.OrdinalPosition != tCol.OrdinalPosition {
				ordinalDiff = true
			}
		}
	}

	if !hasColDiff && ordinalDiff && len(sTbl.Columns) == len(tTbl.Columns) {
		notes = append(notes, fmt.Sprintf("%s: column order differs (informational, not drift)", tblKey))
	}

	// 2. Indexes
	sIndexes := make(map[string]generate.IndexSchema, len(sTbl.Indexes))
	for _, idx := range sTbl.Indexes {
		sIndexes[idx.Name] = idx
	}
	tIndexes := make(map[string]generate.IndexSchema, len(tTbl.Indexes))
	for _, idx := range tTbl.Indexes {
		tIndexes[idx.Name] = idx
	}

	allIdxNamesMap := make(map[string]struct{})
	for k := range sIndexes {
		allIdxNamesMap[k] = struct{}{}
	}
	for k := range tIndexes {
		allIdxNamesMap[k] = struct{}{}
	}
	idxNames := make([]string, 0, len(allIdxNamesMap))
	for k := range allIdxNamesMap {
		idxNames = append(idxNames, k)
	}
	sort.Strings(idxNames)

	for _, idxName := range idxNames {
		sIdx, inScratch := sIndexes[idxName]
		tIdx, inTarget := tIndexes[idxName]

		switch {
		case inScratch && !inTarget:
			findings = append(findings, Finding{
				Kind:   KindMissing,
				Object: ObjectIndex,
				Table:  tblKey,
				Name:   idxName,
				Detail: "index exists in migrations but missing in target",
			})
		case !inScratch && inTarget:
			findings = append(findings, Finding{
				Kind:   KindExtra,
				Object: ObjectIndex,
				Table:  tblKey,
				Name:   idxName,
				Detail: "index exists in target but missing in migrations",
			})
		case inScratch && inTarget:
			var diffs []string
			if sIdx.Unique != tIdx.Unique {
				diffs = append(diffs, fmt.Sprintf("unique %t vs %t", sIdx.Unique, tIdx.Unique))
			}
			if !equalStringSlices(sIdx.Columns, tIdx.Columns) {
				diffs = append(diffs, fmt.Sprintf("columns %v vs %v", sIdx.Columns, tIdx.Columns))
			}
			if len(diffs) > 0 {
				findings = append(findings, Finding{
					Kind:   KindChanged,
					Object: ObjectIndex,
					Table:  tblKey,
					Name:   idxName,
					Detail: strings.Join(diffs, "; "),
				})
			}
		}
	}

	// 3. Foreign Keys
	sFKs := make(map[string]generate.ForeignKeySchema, len(sTbl.ForeignKeys))
	for _, fk := range sTbl.ForeignKeys {
		sFKs[fk.Name] = fk
	}
	tFKs := make(map[string]generate.ForeignKeySchema, len(tTbl.ForeignKeys))
	for _, fk := range tTbl.ForeignKeys {
		tFKs[fk.Name] = fk
	}

	allFKNamesMap := make(map[string]struct{})
	for k := range sFKs {
		allFKNamesMap[k] = struct{}{}
	}
	for k := range tFKs {
		allFKNamesMap[k] = struct{}{}
	}
	fkNames := make([]string, 0, len(allFKNamesMap))
	for k := range allFKNamesMap {
		fkNames = append(fkNames, k)
	}
	sort.Strings(fkNames)

	for _, fkName := range fkNames {
		sFK, inScratch := sFKs[fkName]
		tFK, inTarget := tFKs[fkName]

		switch {
		case inScratch && !inTarget:
			findings = append(findings, Finding{
				Kind:   KindMissing,
				Object: ObjectForeignKey,
				Table:  tblKey,
				Name:   fkName,
				Detail: "foreign key exists in migrations but missing in target",
			})
		case !inScratch && inTarget:
			findings = append(findings, Finding{
				Kind:   KindExtra,
				Object: ObjectForeignKey,
				Table:  tblKey,
				Name:   fkName,
				Detail: "foreign key exists in target but missing in migrations",
			})
		case inScratch && inTarget:
			var diffs []string
			if sFK.RefSchema != tFK.RefSchema {
				diffs = append(diffs, fmt.Sprintf("ref schema %s vs %s", sFK.RefSchema, tFK.RefSchema))
			}
			if sFK.RefTable != tFK.RefTable {
				diffs = append(diffs, fmt.Sprintf("ref table %s vs %s", sFK.RefTable, tFK.RefTable))
			}
			if !equalStringSlices(sFK.Columns, tFK.Columns) {
				diffs = append(diffs, fmt.Sprintf("columns %v vs %v", sFK.Columns, tFK.Columns))
			}
			if !equalStringSlices(sFK.RefColumns, tFK.RefColumns) {
				diffs = append(diffs, fmt.Sprintf("ref columns %v vs %v", sFK.RefColumns, tFK.RefColumns))
			}
			if len(diffs) > 0 {
				findings = append(findings, Finding{
					Kind:   KindChanged,
					Object: ObjectForeignKey,
					Table:  tblKey,
					Name:   fkName,
					Detail: strings.Join(diffs, "; "),
				})
			}
		}
	}

	// 4. Check Constraints
	sCKs := make(map[string]generate.CheckConstraintSchema, len(sTbl.CheckConstraints))
	for _, ck := range sTbl.CheckConstraints {
		sCKs[ck.Name] = ck
	}
	tCKs := make(map[string]generate.CheckConstraintSchema, len(tTbl.CheckConstraints))
	for _, ck := range tTbl.CheckConstraints {
		tCKs[ck.Name] = ck
	}

	allCKNamesMap := make(map[string]struct{})
	for k := range sCKs {
		allCKNamesMap[k] = struct{}{}
	}
	for k := range tCKs {
		allCKNamesMap[k] = struct{}{}
	}
	ckNames := make([]string, 0, len(allCKNamesMap))
	for k := range allCKNamesMap {
		ckNames = append(ckNames, k)
	}
	sort.Strings(ckNames)

	for _, ckName := range ckNames {
		sCK, inScratch := sCKs[ckName]
		tCK, inTarget := tCKs[ckName]

		switch {
		case inScratch && !inTarget:
			findings = append(findings, Finding{
				Kind:   KindMissing,
				Object: ObjectCheck,
				Table:  tblKey,
				Name:   ckName,
				Detail: "check constraint exists in migrations but missing in target",
			})
		case !inScratch && inTarget:
			findings = append(findings, Finding{
				Kind:   KindExtra,
				Object: ObjectCheck,
				Table:  tblKey,
				Name:   ckName,
				Detail: "check constraint exists in target but missing in migrations",
			})
		case inScratch && inTarget:
			if sCK.Expression != tCK.Expression {
				findings = append(findings, Finding{
					Kind:   KindChanged,
					Object: ObjectCheck,
					Table:  tblKey,
					Name:   ckName,
					Detail: fmt.Sprintf("expression %q vs %q", sCK.Expression, tCK.Expression),
				})
			}
		}
	}

	return findings, notes
}
