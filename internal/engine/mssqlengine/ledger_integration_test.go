//go:build integration

package mssqlengine

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/seanpham99/dbtools/internal/ledger"
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
	db, err := Open(url)
	if err != nil {
		t.Fatalf("Open() returned error: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestEnsureSchema_Idempotent(t *testing.T) {
	db := openTestDB(t)

	if err := EnsureSchema(db, "dbtools_migration_history"); err != nil {
		t.Fatalf("EnsureSchema() returned error: %v", err)
	}
	if err := EnsureSchema(db, "dbtools_migration_history"); err != nil {
		t.Fatalf("second EnsureSchema() returned error: %v", err)
	}
}

func TestBackfillAndList(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db, "dbtools_migration_history"); err != nil {
		t.Fatal(err)
	}

	if err := Backfill(db, 20260102000000, true, []uint64{20260101000000, 20260102000000, 20260103000000}, "dbtools_migration_history"); err != nil {
		t.Fatalf("Backfill() returned error: %v", err)
	}

	entries, err := List(db, "dbtools_migration_history")
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("List() returned %d entries, want 2 (versions <= current only)", len(entries))
	}
	if entries[0].Version != 20260101000000 || entries[0].Status != ledger.StatusApplied {
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
	if err := EnsureSchema(db, "dbtools_migration_history"); err != nil {
		t.Fatal(err)
	}
	if err := Backfill(db, 0, false, []uint64{20260101000000}, "dbtools_migration_history"); err != nil {
		t.Fatalf("Backfill() returned error: %v", err)
	}
	entries, err := List(db, "dbtools_migration_history")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("List() = %+v, want empty when hasVersion=false", entries)
	}
}

func TestSetStatusInsertsAndUpdates(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db, "dbtools_migration_history"); err != nil {
		t.Fatal(err)
	}

	if err := SetStatus(db, 20260101000000, ledger.StatusApplied, "test insert", "dbtools_migration_history"); err != nil {
		t.Fatalf("SetStatus() (insert) returned error: %v", err)
	}
	entries, err := List(db, "dbtools_migration_history")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Status != ledger.StatusApplied || entries[0].RecordedAt == nil {
		t.Fatalf("after insert: entries = %+v, want one applied row with non-nil RecordedAt", entries)
	}

	if err := SetStatus(db, 20260101000000, ledger.StatusReverted, "test update", "dbtools_migration_history"); err != nil {
		t.Fatalf("SetStatus() (update) returned error: %v", err)
	}
	entries, err = List(db, "dbtools_migration_history")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Status != ledger.StatusReverted || entries[0].Note != "test update" {
		t.Fatalf("after update: entries = %+v, want one reverted row noted 'test update'", entries)
	}
}

func TestAppliedVersions(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db, "dbtools_migration_history"); err != nil {
		t.Fatal(err)
	}
	if err := SetStatus(db, 20260101000000, ledger.StatusApplied, "", "dbtools_migration_history"); err != nil {
		t.Fatal(err)
	}
	if err := SetStatus(db, 20260102000000, ledger.StatusReverted, "", "dbtools_migration_history"); err != nil {
		t.Fatal(err)
	}
	if err := SetStatus(db, 20260103000000, ledger.StatusApplied, "", "dbtools_migration_history"); err != nil {
		t.Fatal(err)
	}

	versions, err := AppliedVersions(db, "dbtools_migration_history")
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

	db, err := Open(url)
	if err != nil {
		t.Fatalf("Open() returned error: %v", err)
	}
	defer db.Close()

	if err := Sync(db, m, dir, ".up.sql", "dbtools_migration_history"); err != nil {
		t.Fatalf("Sync() returned error: %v", err)
	}

	entries, err := List(db, "dbtools_migration_history")
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("List() returned %d entries, want 2", len(entries))
	}
	for _, e := range entries {
		if e.Status != ledger.StatusApplied {
			t.Errorf("entry %+v: want status applied", e)
		}
	}
}

func TestSetStatusAdopted(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db, "dbtools_migration_history"); err != nil {
		t.Fatal(err)
	}

	if err := SetStatusAdopted(db, 20260101000000, "adopted from schema_migrations", "abc123", "dbtools_migration_history"); err != nil {
		t.Fatalf("SetStatusAdopted() returned error: %v", err)
	}

	entries, err := List(db, "dbtools_migration_history")
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	e := entries[0]
	if e.Status != ledger.StatusApplied {
		t.Errorf("Status = %q, want applied", e.Status)
	}
	if e.HashSource != ledger.HashSourceAdopted {
		t.Errorf("HashSource = %q, want %q", e.HashSource, ledger.HashSourceAdopted)
	}
	if e.ContentSHA256 != "abc123" {
		t.Errorf("ContentSHA256 = %q, want abc123", e.ContentSHA256)
	}
}

func TestEnsureSchema_AddsHashSourceToPreexistingTable(t *testing.T) {
	url := os.Getenv("DBTOOLS_TEST_MSSQL_URL")
	if url == "" {
		t.Skip("DBTOOLS_TEST_MSSQL_URL not set, skipping integration test")
	}
	if err := testdb.ResetTracking(url); err != nil {
		t.Fatal(err)
	}
	db, err := Open(url)
	if err != nil {
		t.Fatalf("Open() returned error: %v", err)
	}
	defer db.Close()

	// Simulate a table created before hash_source existed: the original
	// schema, minus both content_sha256 and hash_source.
	if _, err := db.Exec(`
CREATE TABLE dbtools_migration_history (
    version     BIGINT       NOT NULL PRIMARY KEY,
    status      VARCHAR(10)  NOT NULL CHECK (status IN ('applied', 'reverted')),
    recorded_at DATETIME2(0) NULL,
    note        NVARCHAR(400) NULL
)`); err != nil {
		t.Fatal(err)
	}

	if err := EnsureSchema(db, "dbtools_migration_history"); err != nil {
		t.Fatalf("EnsureSchema() on preexisting table returned error: %v", err)
	}
	if err := SetStatusAdopted(db, 20260101000000, "adopted", "abc123", "dbtools_migration_history"); err != nil {
		t.Fatalf("SetStatusAdopted() after EnsureSchema migration returned error: %v", err)
	}
}

func TestSetStatus_WithinTransaction(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db, "dbtools_migration_history"); err != nil {
		t.Fatal(err)
	}

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("db.Begin() returned error: %v", err)
	}
	if err := SetStatus(tx, 20260101000000, ledger.StatusApplied, "via tx, committed", "dbtools_migration_history"); err != nil {
		t.Fatalf("SetStatus() within tx returned error: %v", err)
	}
	if _, err := AppliedVersions(tx, "dbtools_migration_history"); err != nil {
		t.Fatalf("AppliedVersions() within tx returned error: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("tx.Commit() returned error: %v", err)
	}

	versions, err := AppliedVersions(db, "dbtools_migration_history")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0] != 20260101000000 {
		t.Fatalf("AppliedVersions() after commit = %v, want [20260101000000]", versions)
	}

	tx2, err := db.Begin()
	if err != nil {
		t.Fatalf("db.Begin() returned error: %v", err)
	}
	if err := SetStatus(tx2, 20260102000000, ledger.StatusApplied, "via tx, rolled back", "dbtools_migration_history"); err != nil {
		t.Fatalf("SetStatus() within tx2 returned error: %v", err)
	}
	if err := tx2.Rollback(); err != nil {
		t.Fatalf("tx2.Rollback() returned error: %v", err)
	}

	versions, err = AppliedVersions(db, "dbtools_migration_history")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0] != 20260101000000 {
		t.Fatalf("AppliedVersions() after rollback = %v, want unchanged [20260101000000]", versions)
	}
}
