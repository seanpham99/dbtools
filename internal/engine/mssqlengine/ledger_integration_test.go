//go:build integration

package mssqlengine

import (
	"database/sql"
	"os"
	"testing"

	"github.com/seanpham99/dbtools/internal/ledger"
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

func TestSetStatusAdopted(t *testing.T) {
	db := openTestDB(t)
	if err := EnsureSchema(db, "dbtools_migration_history"); err != nil {
		t.Fatal(err)
	}

	if err := SetStatusAdopted(db, 20260101000000, "adopted from schema_migrations", "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", "dbtools_migration_history"); err != nil {
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
	if e.ContentSHA256 != "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08" {
		t.Errorf("ContentSHA256 = %q, want the expected hash", e.ContentSHA256)
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
	if err := SetStatusAdopted(db, 20260101000000, "adopted", "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", "dbtools_migration_history"); err != nil {
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
