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

// sortedKeys returns the union of a's and b's keys, sorted — the shared
// key-union shape every object-kind comparison below needs before it can
// walk scratch/target in a deterministic order.
func sortedKeys[T any](a, b map[string]T) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	for k := range a {
		seen[k] = struct{}{}
	}
	for k := range b {
		seen[k] = struct{}{}
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// diffKeyed walks the sorted union of scratch's and target's keys,
// emitting a MISSING/EXTRA Finding for a key present on only one side,
// and calling diffFn for a key present on both — diffFn returns the
// human-readable differences found (nil/empty means no CHANGED finding).
// label names the object kind for the MISSING/EXTRA messages, e.g. "index".
func diffKeyed[T any](tblKey string, obj ObjectType, label string, scratch, target map[string]T, diffFn func(s, t T) []string) []Finding {
	var findings []Finding
	for _, name := range sortedKeys(scratch, target) {
		s, inScratch := scratch[name]
		t, inTarget := target[name]
		switch {
		case inScratch && !inTarget:
			findings = append(findings, Finding{
				Kind: KindMissing, Object: obj, Table: tblKey, Name: name,
				Detail: fmt.Sprintf("%s exists in migrations but missing in target", label),
			})
		case !inScratch && inTarget:
			findings = append(findings, Finding{
				Kind: KindExtra, Object: obj, Table: tblKey, Name: name,
				Detail: fmt.Sprintf("%s exists in target but missing in migrations", label),
			})
		case inScratch && inTarget:
			if diffs := diffFn(s, t); len(diffs) > 0 {
				findings = append(findings, Finding{
					Kind: KindChanged, Object: obj, Table: tblKey, Name: name,
					Detail: strings.Join(diffs, "; "),
				})
			}
		}
	}
	return findings
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

	for _, tblKey := range sortedKeys(scratchMap, targetMap) {
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
	findings = append(findings, diffKeyed(tblKey, ObjectIndex, "index", sIndexes, tIndexes, func(sIdx, tIdx generate.IndexSchema) []string {
		var diffs []string
		if sIdx.Unique != tIdx.Unique {
			diffs = append(diffs, fmt.Sprintf("unique %t vs %t", sIdx.Unique, tIdx.Unique))
		}
		if !equalStringSlices(sIdx.Columns, tIdx.Columns) {
			diffs = append(diffs, fmt.Sprintf("columns %v vs %v", sIdx.Columns, tIdx.Columns))
		}
		return diffs
	})...)

	// 3. Foreign Keys
	sFKs := make(map[string]generate.ForeignKeySchema, len(sTbl.ForeignKeys))
	for _, fk := range sTbl.ForeignKeys {
		sFKs[fk.Name] = fk
	}
	tFKs := make(map[string]generate.ForeignKeySchema, len(tTbl.ForeignKeys))
	for _, fk := range tTbl.ForeignKeys {
		tFKs[fk.Name] = fk
	}
	findings = append(findings, diffKeyed(tblKey, ObjectForeignKey, "foreign key", sFKs, tFKs, func(sFK, tFK generate.ForeignKeySchema) []string {
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
		return diffs
	})...)

	// 4. Check Constraints
	sCKs := make(map[string]generate.CheckConstraintSchema, len(sTbl.CheckConstraints))
	for _, ck := range sTbl.CheckConstraints {
		sCKs[ck.Name] = ck
	}
	tCKs := make(map[string]generate.CheckConstraintSchema, len(tTbl.CheckConstraints))
	for _, ck := range tTbl.CheckConstraints {
		tCKs[ck.Name] = ck
	}
	findings = append(findings, diffKeyed(tblKey, ObjectCheck, "check constraint", sCKs, tCKs, func(sCK, tCK generate.CheckConstraintSchema) []string {
		if !equalExpressions(sCK.Expression, tCK.Expression) {
			return []string{fmt.Sprintf("expression %q vs %q", sCK.Expression, tCK.Expression)}
		}
		return nil
	})...)

	return findings, notes
}

// equalExpressions compares two catalog-rendered SQL expressions for
// semantic equality, ignoring the two things the server is free to vary:
// redundant outer parentheses and whitespace.
//
// The server decides how to print a stored parse tree, and that rendering is
// not stable. Postgres 16 renders a CHECK constraint as "((total_jobs >= 0))"
// where 17 renders "(total_jobs >= 0)" for the identical constraint. diff
// pins its scratch database to the target's major version to avoid that
// class entirely (see scratchdb.ProvisionMajor), so this is the second line
// of defence: point releases, --against databases the caller provisioned
// themselves, and future rendering changes can all still put a cosmetic
// difference in front of the comparison.
//
// It normalises only formatting. Any difference in operators, literals, or
// identifiers survives and is still reported.
func equalExpressions(a, b string) bool {
	return a == b || normalizeExpression(a) == normalizeExpression(b)
}

// normalizeExpression strips balanced outer parentheses and collapses
// whitespace runs to a single space.
func normalizeExpression(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	for len(s) >= 2 && s[0] == '(' && s[len(s)-1] == ')' && outerParensBalanced(s) {
		s = strings.TrimSpace(s[1 : len(s)-1])
	}
	return s
}

// outerParensBalanced reports whether s's first parenthesis is closed by its
// last one, so they can be stripped as a redundant wrapper. It is false for
// "(a) AND (b)", where the outer pair is not a wrapper at all and removing
// the ends would corrupt the expression.
//
// Parentheses inside string literals are skipped: '(' is a legitimate
// character in a CHECK against text, and counting it would mis-balance.
func outerParensBalanced(s string) bool {
	depth := 0
	inQuote := false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\'':
			// '' inside a quoted literal is an escaped quote, not a close.
			if inQuote && i+1 < len(s) && s[i+1] == '\'' {
				i++
				continue
			}
			inQuote = !inQuote
		case inQuote:
			continue
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 && i != len(s)-1 {
				return false
			}
		}
	}
	return depth == 0 && !inQuote
}
