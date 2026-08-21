//go:build integration

package ledger

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/seanpham99/dbtools/internal/dbconn"
	"github.com/seanpham99/dbtools/internal/migrator"
	"github.com/seanpham99/dbtools/internal/testdb"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("DBTOOLS_TEST_MSSQL_URL")
	if url == "" {
		t.Skip("DBTOOLS_TEST_MSSQL_URL not set, skipping integration test")
	}
	if err := testdb.ResetTracking(url); err != nil {
		t.Fatal(err)
	}
	db, err := dbconn.Open(url)
	if err != nil {
		t.Fatalf("dbconn.Open() returned error: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestEnsureSchema_Idempotent(t *testing.T) {
	db := openTestDB(t)

	if err := EnsureSchema(db); err != nil {
		t.Fatalf("EnsureSchema() returned error: %v", err)
	}
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("second EnsureSchema() returned error: %v", err)
	}
}

func TestBackfillAndList(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatal(err)
	}

	if err := Backfill(db, 20260102000000, true, []uint64{20260101000000, 20260102000000, 20260103000000}); err != nil {
		t.Fatalf("Backfill() returned error: %v", err)
	}

	entries, err := List(db)
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("List() returned %d entries, want 2 (versions <= current only)", len(entries))
	}
	if entries[0].Version != 20260101000000 || entries[0].Status != StatusApplied {
		t.Errorf("entries[0] = %+v, want version=20260101000000 status=applied", entries[0])
	}
	if entries[0].RecordedAt != nil {
		t.Errorf("entries[0].RecordedAt = %v, want nil for a backfilled row", entries[0].RecordedAt)
	}
	if entries[1].Version != 20260102000000 {
		t.Errorf("entries[1].Version = %d, want 20260102000000", entries[1].Version)
	}
}

func TestBackfill_NoVersionYet(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := Backfill(db, 0, false, []uint64{20260101000000}); err != nil {
		t.Fatalf("Backfill() returned error: %v", err)
	}
	entries, err := List(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("List() = %+v, want empty when hasVersion=false", entries)
	}
}

func TestSetStatusInsertsAndUpdates(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatal(err)
	}

	if err := SetStatus(db, 20260101000000, StatusApplied, "test insert"); err != nil {
		t.Fatalf("SetStatus() (insert) returned error: %v", err)
	}
	entries, err := List(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Status != StatusApplied || entries[0].RecordedAt == nil {
		t.Fatalf("after insert: entries = %+v, want one applied row with non-nil RecordedAt", entries)
	}

	if err := SetStatus(db, 20260101000000, StatusReverted, "test update"); err != nil {
		t.Fatalf("SetStatus() (update) returned error: %v", err)
	}
	entries, err = List(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Status != StatusReverted || entries[0].Note != "test update" {
		t.Fatalf("after update: entries = %+v, want one reverted row noted 'test update'", entries)
	}
}

func TestAppliedVersions(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := SetStatus(db, 20260101000000, StatusApplied, ""); err != nil {
		t.Fatal(err)
	}
	if err := SetStatus(db, 20260102000000, StatusReverted, ""); err != nil {
		t.Fatal(err)
	}
	if err := SetStatus(db, 20260103000000, StatusApplied, ""); err != nil {
		t.Fatal(err)
	}

	versions, err := AppliedVersions(db)
	if err != nil {
		t.Fatalf("AppliedVersions() returned error: %v", err)
	}
	want := []uint64{20260101000000, 20260103000000}
	if len(versions) != len(want) || versions[0] != want[0] || versions[1] != want[1] {
		t.Errorf("AppliedVersions() = %v, want %v", versions, want)
	}
}

func TestSync(t *testing.T) {
	url := os.Getenv("DBTOOLS_TEST_MSSQL_URL")
	if url == "" {
		t.Skip("DBTOOLS_TEST_MSSQL_URL not set, skipping integration test")
	}
	if err := testdb.ResetTracking(url); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "20260101000000_create_widgets.up.sql"),
		[]byte("CREATE TABLE dbtools_test_ledger_sync (id INT PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "20260102000000_add_more.up.sql"),
		[]byte("CREATE TABLE dbtools_test_ledger_sync_2 (id INT PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := migrator.Open(url, dir)
	if err != nil {
		t.Fatalf("migrator.Open() returned error: %v", err)
	}
	defer m.Close()
	if _, err := m.Up(); err != nil {
		t.Fatalf("Up() returned error: %v", err)
	}

	db, err := dbconn.Open(url)
	if err != nil {
		t.Fatalf("dbconn.Open() returned error: %v", err)
	}
	defer db.Close()

	if err := Sync(db, m, dir); err != nil {
		t.Fatalf("Sync() returned error: %v", err)
	}

	entries, err := List(db)
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("List() returned %d entries, want 2", len(entries))
	}
	for _, e := range entries {
		if e.Status != StatusApplied {
			t.Errorf("entry %+v: want status applied", e)
		}
	}
}

func TestSetStatus_WithinTransaction(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db); err != nil {
		t.Fatal(err)
	}

	// A committed transaction's writes must be visible afterward — proves
	// SetStatus/AppliedVersions genuinely work against a *sql.Tx (not just
	// *sql.DB), which repair.Run relies on to make its writes atomic.
	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("db.Begin() returned error: %v", err)
	}
	if err := SetStatus(tx, 20260101000000, StatusApplied, "via tx, committed"); err != nil {
		t.Fatalf("SetStatus() within tx returned error: %v", err)
	}
	if _, err := AppliedVersions(tx); err != nil {
		t.Fatalf("AppliedVersions() within tx returned error: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("tx.Commit() returned error: %v", err)
	}

	versions, err := AppliedVersions(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0] != 20260101000000 {
		t.Fatalf("AppliedVersions() after commit = %v, want [20260101000000]", versions)
	}

	// A rolled-back transaction's writes must NOT be visible — proves a
	// mid-repair failure (before commit) leaves no partial state behind.
	tx2, err := db.Begin()
	if err != nil {
		t.Fatalf("db.Begin() returned error: %v", err)
	}
	if err := SetStatus(tx2, 20260102000000, StatusApplied, "via tx, rolled back"); err != nil {
		t.Fatalf("SetStatus() within tx2 returned error: %v", err)
	}
	if err := tx2.Rollback(); err != nil {
		t.Fatalf("tx2.Rollback() returned error: %v", err)
	}

	versions, err = AppliedVersions(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0] != 20260101000000 {
		t.Fatalf("AppliedVersions() after rollback = %v, want unchanged [20260101000000] (20260102000000 must not persist)", versions)
	}
}
