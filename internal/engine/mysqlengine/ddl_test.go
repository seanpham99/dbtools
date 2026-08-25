package mysqlengine

import (
	"testing"

	"github.com/seanpham99/dbtools/internal/ddlcheck"
)

func TestExtractObjects_CreateTableBacktickQuoted(t *testing.T) {
	sql := "CREATE TABLE `widget_order` (\n    `widget_order_id` BIGINT AUTO_INCREMENT PRIMARY KEY\n);"
	got := mysqlDDL{}.ExtractObjects(sql)
	want := ddlcheck.ObjectRef{Schema: "", Name: "widget_order", Kind: "table"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("ExtractObjects() = %+v, want [%+v]", got, want)
	}
}

func TestExtractObjects_CreateTableIfNotExistsUnquoted(t *testing.T) {
	sql := "CREATE TABLE IF NOT EXISTS orders (id INT);"
	got := mysqlDDL{}.ExtractObjects(sql)
	want := ddlcheck.ObjectRef{Schema: "", Name: "orders", Kind: "table"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("ExtractObjects() = %+v, want [%+v]", got, want)
	}
}

func TestExtractObjects_View(t *testing.T) {
	sql := "CREATE OR REPLACE VIEW `active_users` AS SELECT * FROM users;"
	got := mysqlDDL{}.ExtractObjects(sql)
	want := ddlcheck.ObjectRef{Schema: "", Name: "active_users", Kind: "view"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("ExtractObjects() = %+v, want [%+v]", got, want)
	}
}

func TestExtractObjects_AlterAndIndexNotExtracted(t *testing.T) {
	sql := "ALTER TABLE widget_order ADD amount DECIMAL(19,6);\nCREATE INDEX ix_amount ON widget_order(amount);"
	got := mysqlDDL{}.ExtractObjects(sql)
	if len(got) != 0 {
		t.Errorf("ExtractObjects() = %+v, want 0 objects (ALTER/INDEX out of scope)", got)
	}
}

func TestExtractDroppedObjects_GuardedDrop(t *testing.T) {
	sql := "DROP TABLE IF EXISTS `legacy_widget_tracking`;"
	got := mysqlDDL{}.ExtractDroppedObjects(sql)
	want := ddlcheck.ObjectRef{Schema: "", Name: "legacy_widget_tracking", Kind: "table"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("ExtractDroppedObjects() = %+v, want [%+v]", got, want)
	}
}

func TestExtractDroppedObjects_View(t *testing.T) {
	sql := "DROP VIEW active_users;"
	got := mysqlDDL{}.ExtractDroppedObjects(sql)
	want := ddlcheck.ObjectRef{Schema: "", Name: "active_users", Kind: "view"}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("ExtractDroppedObjects() = %+v, want [%+v]", got, want)
	}
}

func TestExistsRejectsUnknownKind(t *testing.T) {
	if _, err := (mysqlDDL{}).Exists(nil, ddlcheck.ObjectRef{Kind: "procedure"}); err == nil {
		t.Fatal("Exists() with an out-of-scope kind should fail")
	}
}
