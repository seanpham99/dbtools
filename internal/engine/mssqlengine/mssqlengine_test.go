package mssqlengine

import (
	"testing"

	"github.com/seanpham99/dbtools/internal/engine"
)

// The MSSQL engine registers itself in init(); mssql:// URLs must resolve
// to it, and its dialect hooks must be wired.
func TestMSSQLRegistered(t *testing.T) {
	e, err := engine.ForURL("mssql://sa:x@127.0.0.1:1433?database=master")
	if err != nil {
		t.Fatalf("ForURL(mssql://...) returned error: %v", err)
	}
	if e.Name() != "mssql" {
		t.Fatalf("resolved engine %q, want mssql", e.Name())
	}
	if e.DDL() == nil || e.Ledger() == nil {
		t.Fatal("MSSQL engine must provide DDL and Ledger dialects")
	}

	objs := e.DDL().ExtractObjects("CREATE TABLE [dbo].[users] (id INT);")
	if len(objs) != 1 || objs[0].Name != "users" || objs[0].Schema != "dbo" {
		t.Fatalf("DDL().ExtractObjects() = %+v, want one dbo.users ref", objs)
	}
}
