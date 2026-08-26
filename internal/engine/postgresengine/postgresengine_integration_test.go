//go:build integration

package postgresengine

import (
	"os"
	"testing"

	"github.com/seanpham99/dbtools/internal/ddlcheck"
	"github.com/seanpham99/dbtools/internal/ledger"
)

// TestLiveLedgerDDLAndIntrospection exercises the Postgres engine against
// a real server: ledger create/upsert/list, DDL existence checks, and
// information_schema introspection.
func TestLiveLedgerDDLAndIntrospection(t *testing.T) {
	rawURL := os.Getenv("DBTOOLS_TEST_POSTGRES_URL")
	if rawURL == "" {
		t.Skip("DBTOOLS_TEST_POSTGRES_URL not set, skipping integration test")
	}

	eng := Postgres{}
	db, err := eng.Open(rawURL)
	if err != nil {
		t.Fatalf("Open() returned error: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatalf("Ping() returned error: %v", err)
	}

	// Ledger round trip.
	t.Cleanup(func() { db.Exec(`DROP TABLE IF EXISTS dbtools_migration_history`) })
	store := ledgerStore{}
	if err := store.ensureSchema(db, "dbtools_migration_history"); err != nil {
		t.Fatalf("ensureSchema() returned error: %v", err)
	}
	if err := store.ensureSchema(db, "dbtools_migration_history"); err != nil {
		t.Fatalf("second ensureSchema() should be idempotent: %v", err)
	}
	if err := store.SetStatus(db, 42, ledger.StatusApplied, "first", "dbtools_migration_history"); err != nil {
		t.Fatalf("SetStatus(insert) returned error: %v", err)
	}
	if err := store.SetStatus(db, 42, ledger.StatusReverted, "second", "dbtools_migration_history"); err != nil {
		t.Fatalf("SetStatus(upsert) returned error: %v", err)
	}
	entries, err := store.List(db, "dbtools_migration_history")
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if len(entries) != 1 || entries[0].Version != 42 || entries[0].Status != ledger.StatusReverted || entries[0].Note != "second" {
		t.Fatalf("List() = %+v, want one reverted v42 row noted 'second'", entries)
	}
	if entries[0].RecordedAt == nil {
		t.Fatal("List() row RecordedAt = nil, want a timestamp")
	}
	if err := store.backfill(db, 100, true, []uint64{42, 99, 150}, "dbtools_migration_history"); err != nil {
		t.Fatalf("backfill() returned error: %v", err)
	}
	applied, err := store.AppliedVersions(db, "dbtools_migration_history")
	if err != nil {
		t.Fatalf("AppliedVersions() returned error: %v", err)
	}
	// 42 stays reverted (ON CONFLICT DO NOTHING), 99 backfilled, 150 above cursor.
	if len(applied) != 1 || applied[0] != 99 {
		t.Fatalf("AppliedVersions() = %v, want [99]", applied)
	}

	// DDL existence + introspection.
	t.Cleanup(func() { db.Exec(`DROP TABLE IF EXISTS dbtools_it_probe`) })
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS dbtools_it_probe (id bigint PRIMARY KEY, name text NULL, created_at timestamptz NOT NULL)`); err != nil {
		t.Fatalf("creating probe table: %v", err)
	}
	exists, err := eng.DDL().Exists(db, ddlcheck.ObjectRef{Schema: "public", Name: "dbtools_it_probe", Kind: "table"})
	if err != nil || !exists {
		t.Fatalf("Exists(probe table) = %v, %v; want true, nil", exists, err)
	}
	exists, err = eng.DDL().Exists(db, ddlcheck.ObjectRef{Schema: "public", Name: "dbtools_never_created", Kind: "view"})
	if err != nil || exists {
		t.Fatalf("Exists(missing view) = %v, %v; want false, nil", exists, err)
	}

	tables, unmapped, err := eng.Introspect(db, []string{"dbtools_migration_history"})
	if err != nil {
		t.Fatalf("Introspect() returned error: %v", err)
	}
	var probe *int
	for i, tbl := range tables {
		if tbl.Schema == "public" && tbl.Name == "dbtools_it_probe" {
			probe = &i
			break
		}
		if tbl.Name == "dbtools_migration_history" {
			t.Fatal("Introspect() returned an excluded table")
		}
	}
	if probe == nil {
		t.Fatalf("Introspect() missing dbtools_it_probe (unmapped: %v)", unmapped)
	}
	cols := tables[*probe].Columns
	if len(cols) != 3 || cols[0].PythonType != "int" || cols[1].PythonType != "str" || !cols[1].IsNullable || cols[2].PythonType != "datetime" {
		t.Fatalf("Introspect() probe columns = %+v", cols)
	}
}
