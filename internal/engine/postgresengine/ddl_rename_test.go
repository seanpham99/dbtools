package postgresengine

import (
	"testing"

	"github.com/seanpham99/dbtools/internal/ddlcheck"
)

func TestExtractRenamedObjects_RebuildInPlaceLastStep(t *testing.T) {
	sql := "CREATE TABLE widget_new (id INTEGER PRIMARY KEY);\n" +
		"DROP TABLE widget;\n" +
		"ALTER TABLE widget_new RENAME TO widget;"
	got := ddl{}.ExtractRenamedObjects(sql)
	want := []ddlcheck.Rename{{
		From: ddlcheck.ObjectRef{Schema: "public", Name: "widget_new", Kind: "table"},
		To:   ddlcheck.ObjectRef{Schema: "public", Name: "widget", Kind: "table"},
	}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("ExtractRenamedObjects() = %+v, want [%+v]", got, want)
	}
}

func TestExtractRenamedObjects_SchemaQualifiedSourceKeepsSchemaOnTarget(t *testing.T) {
	got := ddl{}.ExtractRenamedObjects("ALTER TABLE app.widget_new RENAME TO widget;")
	want := ddlcheck.Rename{
		From: ddlcheck.ObjectRef{Schema: "app", Name: "widget_new", Kind: "table"},
		To:   ddlcheck.ObjectRef{Schema: "app", Name: "widget", Kind: "table"},
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("ExtractRenamedObjects() = %+v, want [%+v]", got, want)
	}
}

func TestExtractRenamedObjects_QuotedIdentifiers(t *testing.T) {
	got := ddl{}.ExtractRenamedObjects(`ALTER TABLE "widget_new" RENAME TO "widget_old";`)
	want := ddlcheck.Rename{
		From: ddlcheck.ObjectRef{Schema: "public", Name: "widget_new", Kind: "table"},
		To:   ddlcheck.ObjectRef{Schema: "public", Name: "widget_old", Kind: "table"},
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("ExtractRenamedObjects() = %+v, want [%+v]", got, want)
	}
}

func TestExtractRenamedObjects_ViewAndMaterializedView(t *testing.T) {
	sql := "ALTER VIEW active_users RENAME TO users_v;\n" +
		"ALTER MATERIALIZED VIEW rollup_v RENAME TO rollup;"
	got := ddl{}.ExtractRenamedObjects(sql)
	if len(got) != 2 {
		t.Fatalf("ExtractRenamedObjects() = %+v, want 2 renames", got)
	}
	if got[0].From.Kind != "view" || got[0].To.Name != "users_v" {
		t.Errorf("first rename = %+v, want a view renamed to users_v", got[0])
	}
	if got[1].From.Kind != "view" || got[1].To.Name != "rollup" {
		t.Errorf("second rename = %+v, want a view renamed to rollup", got[1])
	}
}

func TestExtractRenamedObjects_OnlyTargetAndIfExistsAndOnly(t *testing.T) {
	sql := "ALTER TABLE IF EXISTS ONLY widget_new RENAME TO widget;"
	got := ddl{}.ExtractRenamedObjects(sql)
	if len(got) != 1 || got[0].From.Name != "widget_new" || got[0].To.Name != "widget" {
		t.Fatalf("ExtractRenamedObjects() = %+v, want widget_new renamed to widget", got)
	}
}

func TestExtractRenamedObjects_RenameColumnIsNotAnObjectRename(t *testing.T) {
	got := ddl{}.ExtractRenamedObjects("ALTER TABLE widget RENAME COLUMN amount TO total;")
	if len(got) != 0 {
		t.Errorf("ExtractRenamedObjects() = %+v, want 0 renames — a column is not a tracked object", got)
	}
}

func TestExtractRenamedObjects_InsideDollarQuotedBodyIsIgnored(t *testing.T) {
	sql := "CREATE FUNCTION rebuild() RETURNS void AS $$\n" +
		"  ALTER TABLE widget_new RENAME TO widget;\n" +
		"$$ LANGUAGE plpgsql;"
	got := ddl{}.ExtractRenamedObjects(sql)
	if len(got) != 0 {
		t.Errorf("ExtractRenamedObjects() = %+v, want 0 renames — the statement is in a function body", got)
	}
}

func TestExtractRenamedObjects_UnrelatedAlterIsNotARename(t *testing.T) {
	sql := "ALTER TABLE widget ADD COLUMN amount INTEGER;\nCREATE INDEX ix_widget ON widget(amount);"
	got := ddl{}.ExtractRenamedObjects(sql)
	if len(got) != 0 {
		t.Errorf("ExtractRenamedObjects() = %+v, want 0 renames", got)
	}
}
