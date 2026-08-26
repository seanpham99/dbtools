package diff_test

import (
	"testing"

	"github.com/seanpham99/dbtools/internal/diff"
	"github.com/seanpham99/dbtools/internal/generate"
)

func TestCompare_MissingTable(t *testing.T) {
	scratch := []generate.TableSchema{{Schema: "public", Name: "orders"}}
	target := []generate.TableSchema{}
	findings, _ := diff.Compare(scratch, target)
	if len(findings) != 1 || findings[0].Kind != diff.KindMissing || findings[0].Object != diff.ObjectTable {
		t.Fatalf("findings = %+v, want one MISSING table", findings)
	}
}

func TestCompare_ExtraTable(t *testing.T) {
	scratch := []generate.TableSchema{}
	target := []generate.TableSchema{{Schema: "public", Name: "orders"}}
	findings, _ := diff.Compare(scratch, target)
	if len(findings) != 1 || findings[0].Kind != diff.KindExtra || findings[0].Object != diff.ObjectTable {
		t.Fatalf("findings = %+v, want one EXTRA table", findings)
	}
}

func TestCompare_ColumnTypeChanged(t *testing.T) {
	scratch := []generate.TableSchema{{Schema: "public", Name: "orders", Columns: []generate.ColumnSchema{
		{Name: "status", DataType: "text"},
	}}}
	target := []generate.TableSchema{{Schema: "public", Name: "orders", Columns: []generate.ColumnSchema{
		{Name: "status", DataType: "varchar"},
	}}}
	findings, _ := diff.Compare(scratch, target)
	if len(findings) != 1 || findings[0].Kind != diff.KindChanged || findings[0].Object != diff.ObjectColumn {
		t.Fatalf("findings = %+v, want one CHANGED column", findings)
	}
}

func TestCompare_ColumnMissing(t *testing.T) {
	scratch := []generate.TableSchema{{Schema: "public", Name: "orders", Columns: []generate.ColumnSchema{
		{Name: "status", DataType: "text"},
	}}}
	target := []generate.TableSchema{{Schema: "public", Name: "orders"}}
	findings, _ := diff.Compare(scratch, target)
	if len(findings) != 1 || findings[0].Kind != diff.KindMissing || findings[0].Object != diff.ObjectColumn {
		t.Fatalf("findings = %+v, want one MISSING column", findings)
	}
}

func TestCompare_IndexChanged(t *testing.T) {
	scratch := []generate.TableSchema{{Schema: "public", Name: "orders", Indexes: []generate.IndexSchema{
		{Name: "idx_status", Columns: []string{"status"}, Unique: true},
	}}}
	target := []generate.TableSchema{{Schema: "public", Name: "orders", Indexes: []generate.IndexSchema{
		{Name: "idx_status", Columns: []string{"status"}, Unique: false},
	}}}
	findings, _ := diff.Compare(scratch, target)
	if len(findings) != 1 || findings[0].Kind != diff.KindChanged || findings[0].Object != diff.ObjectIndex {
		t.Fatalf("findings = %+v, want one CHANGED index (unique flag)", findings)
	}
}

func TestCompare_ForeignKeyExtra(t *testing.T) {
	scratch := []generate.TableSchema{{Schema: "public", Name: "orders"}}
	target := []generate.TableSchema{{Schema: "public", Name: "orders", ForeignKeys: []generate.ForeignKeySchema{
		{Name: "fk_customer", Columns: []string{"customer_id"}, RefTable: "customers", RefColumns: []string{"id"}},
	}}}
	findings, _ := diff.Compare(scratch, target)
	if len(findings) != 1 || findings[0].Kind != diff.KindExtra || findings[0].Object != diff.ObjectForeignKey {
		t.Fatalf("findings = %+v, want one EXTRA foreign key", findings)
	}
}

func TestCompare_CheckConstraintChanged(t *testing.T) {
	scratch := []generate.TableSchema{{Schema: "public", Name: "orders", CheckConstraints: []generate.CheckConstraintSchema{
		{Name: "chk_total", Expression: "total >= 0"},
	}}}
	target := []generate.TableSchema{{Schema: "public", Name: "orders", CheckConstraints: []generate.CheckConstraintSchema{
		{Name: "chk_total", Expression: "total > 0"},
	}}}
	findings, _ := diff.Compare(scratch, target)
	if len(findings) != 1 || findings[0].Kind != diff.KindChanged || findings[0].Object != diff.ObjectCheck {
		t.Fatalf("findings = %+v, want one CHANGED check constraint", findings)
	}
}

func TestCompare_OrdinalPositionIsInformationalNotDrift(t *testing.T) {
	scratch := []generate.TableSchema{{Schema: "public", Name: "orders", Columns: []generate.ColumnSchema{
		{Name: "a", DataType: "int", OrdinalPosition: 1},
		{Name: "b", DataType: "int", OrdinalPosition: 2},
	}}}
	target := []generate.TableSchema{{Schema: "public", Name: "orders", Columns: []generate.ColumnSchema{
		{Name: "a", DataType: "int", OrdinalPosition: 2},
		{Name: "b", DataType: "int", OrdinalPosition: 1},
	}}}
	findings, notes := diff.Compare(scratch, target)
	if len(findings) != 0 {
		t.Errorf("findings = %+v, want zero — ordinal position alone is not drift", findings)
	}
	if len(notes) == 0 {
		t.Error("notes is empty, want at least one informational note about column order")
	}
}

func TestCompare_IdenticalSchemasProduceNoFindings(t *testing.T) {
	tbl := generate.TableSchema{Schema: "public", Name: "orders", Columns: []generate.ColumnSchema{
		{Name: "id", DataType: "int", OrdinalPosition: 1, IsPrimaryKey: true},
	}}
	findings, notes := diff.Compare([]generate.TableSchema{tbl}, []generate.TableSchema{tbl})
	if len(findings) != 0 || len(notes) != 0 {
		t.Fatalf("findings = %+v, notes = %v, want both empty for identical schemas", findings, notes)
	}
}
