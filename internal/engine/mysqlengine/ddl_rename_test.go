package mysqlengine

import (
	"testing"

	"github.com/seanpham99/dbtools/internal/ddlcheck"
)

func TestExtractRenamedObjects_RebuildInPlaceLastStep(t *testing.T) {
	sql := "CREATE TABLE `widget_new` (id BIGINT PRIMARY KEY);\n" +
		"DROP TABLE widget;\n" +
		"ALTER TABLE `widget_new` RENAME TO widget;"
	got := mysqlDDL{}.ExtractRenamedObjects(sql)
	want := ddlcheck.Rename{
		From: ddlcheck.ObjectRef{Schema: "", Name: "widget_new", Kind: "table"},
		To:   ddlcheck.ObjectRef{Schema: "", Name: "widget", Kind: "table"},
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("ExtractRenamedObjects() = %+v, want [%+v]", got, want)
	}
}

func TestExtractRenamedObjects_QualifiedSourceKeepsDatabaseOnTarget(t *testing.T) {
	got := mysqlDDL{}.ExtractRenamedObjects("ALTER TABLE shop.widget_new RENAME TO widget;")
	want := ddlcheck.Rename{
		From: ddlcheck.ObjectRef{Schema: "shop", Name: "widget_new", Kind: "table"},
		To:   ddlcheck.ObjectRef{Schema: "shop", Name: "widget", Kind: "table"},
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("ExtractRenamedObjects() = %+v, want [%+v]", got, want)
	}
}

func TestExtractRenamedObjects_QualifiedTargetMovesToThatDatabase(t *testing.T) {
	got := mysqlDDL{}.ExtractRenamedObjects("ALTER TABLE shop.widget_new RENAME TO archive.widget;")
	want := ddlcheck.Rename{
		From: ddlcheck.ObjectRef{Schema: "shop", Name: "widget_new", Kind: "table"},
		To:   ddlcheck.ObjectRef{Schema: "archive", Name: "widget", Kind: "table"},
	}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("ExtractRenamedObjects() = %+v, want [%+v]", got, want)
	}
}

func TestExtractRenamedObjects_RenameAsIsAlsoARename(t *testing.T) {
	got := mysqlDDL{}.ExtractRenamedObjects("ALTER VIEW active_users RENAME AS users_v;")
	if len(got) != 1 || got[0].From.Kind != "view" || got[0].To.Name != "users_v" {
		t.Fatalf("ExtractRenamedObjects() = %+v, want active_users renamed to users_v", got)
	}
}

func TestExtractRenamedObjects_UnrelatedAlterIsNotARename(t *testing.T) {
	sql := "ALTER TABLE widget_order ADD amount DECIMAL(19,6);\n" +
		"RENAME TABLE widget_old TO widget_archive;"
	got := mysqlDDL{}.ExtractRenamedObjects(sql)
	if len(got) != 0 {
		t.Errorf("ExtractRenamedObjects() = %+v, want 0 renames — only ALTER … RENAME is in scope", got)
	}
}
