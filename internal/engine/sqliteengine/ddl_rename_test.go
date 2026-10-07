package sqliteengine

import (
	"testing"

	"github.com/seanpham99/dbtools/internal/ddlcheck"
)

func TestExtractRenamedObjects_RebuildInPlaceLastStep(t *testing.T) {
	sql := "CREATE TABLE widget_new (id INTEGER PRIMARY KEY, n INTEGER NOT NULL CHECK (n >= 0));\n" +
		"INSERT INTO widget_new SELECT * FROM widget;\n" +
		"DROP TABLE widget;\n" +
		"ALTER TABLE widget_new RENAME TO widget;"
	got := ddl{}.ExtractRenamedObjects(sql)
	want := ddlcheck.Rename{
		From: ddlcheck.ObjectRef{Schema: "main", Name: "widget_new", Kind: "table"},
		To:   ddlcheck.ObjectRef{Schema: "main", Name: "widget", Kind: "table"},
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("ExtractRenamedObjects() = %+v, want [%+v]", got, want)
	}
}

func TestExtractRenamedObjects_SchemaQualifiedSourceKeepsSchemaOnTarget(t *testing.T) {
	got := ddl{}.ExtractRenamedObjects("ALTER TABLE aux.widget_new RENAME TO widget;")
	want := ddlcheck.Rename{
		From: ddlcheck.ObjectRef{Schema: "aux", Name: "widget_new", Kind: "table"},
		To:   ddlcheck.ObjectRef{Schema: "aux", Name: "widget", Kind: "table"},
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("ExtractRenamedObjects() = %+v, want [%+v]", got, want)
	}
}

func TestExtractRenamedObjects_QuotedIdentifiers(t *testing.T) {
	got := ddl{}.ExtractRenamedObjects(`ALTER TABLE "widget_new" RENAME TO "widget_old";`)
	want := ddlcheck.Rename{
		From: ddlcheck.ObjectRef{Schema: "main", Name: "widget_new", Kind: "table"},
		To:   ddlcheck.ObjectRef{Schema: "main", Name: "widget_old", Kind: "table"},
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("ExtractRenamedObjects() = %+v, want [%+v]", got, want)
	}
}

func TestExtractRenamedObjects_MultipleRenamesInOneFile(t *testing.T) {
	sql := "ALTER TABLE a_new RENAME TO a;\nALTER TABLE b_new RENAME TO b;"
	got := ddl{}.ExtractRenamedObjects(sql)
	if len(got) != 2 || got[0].To.Name != "a" || got[1].To.Name != "b" {
		t.Fatalf("ExtractRenamedObjects() = %+v, want both renames in source order", got)
	}
}

func TestExtractRenamedObjects_RenameColumnAndUnrelatedAlterAreNotRenames(t *testing.T) {
	sql := "ALTER TABLE widget RENAME COLUMN amount TO total;\n" +
		"ALTER TABLE widget ADD COLUMN extra TEXT;\n" +
		"CREATE TABLE other (id INTEGER PRIMARY KEY);"
	got := ddl{}.ExtractRenamedObjects(sql)
	if len(got) != 0 {
		t.Errorf("ExtractRenamedObjects() = %+v, want 0 renames", got)
	}
}
