package mysqlengine

import (
	"testing"

	"github.com/seanpham99/dbtools/internal/engine"
)

// The MySQL engine registers itself in init(); mysql:// URLs must resolve
// to it, and its dialect hooks must be wired.
func TestMySQLRegistered(t *testing.T) {
	e, err := engine.ForURL("mysql://root:x@tcp(127.0.0.1:3306)/dbtools_test")
	if err != nil {
		t.Fatalf("ForURL(mysql://...) returned error: %v", err)
	}
	if e.Name() != "mysql" {
		t.Fatalf("resolved engine %q, want mysql", e.Name())
	}
	if e.DDL() == nil || e.Ledger() == nil {
		t.Fatal("MySQL engine must provide DDL and Ledger dialects")
	}

	objs := e.DDL().ExtractObjects("CREATE TABLE `users` (id INT);")
	if len(objs) != 1 || objs[0].Name != "users" {
		t.Fatalf("DDL().ExtractObjects() = %+v, want one users ref", objs)
	}
}

func TestForTargetValidatesConfiguredEngine(t *testing.T) {
	if _, err := engine.ForTarget("mysql", "mysql://u:p@tcp(h:3306)/db"); err != nil {
		t.Fatalf("ForTarget(mysql, mysql://) returned error: %v", err)
	}
	if _, err := engine.ForTarget("mysql", "postgres://u:p@h/db"); err == nil {
		t.Fatal("ForTarget(mysql, postgres://) should fail")
	}
}
