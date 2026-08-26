//go:build integration

package mysqlengine

import (
	"database/sql"
	"os"
	"testing"

	"github.com/seanpham99/dbtools/internal/ledger"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("DBTOOLS_TEST_MYSQL_URL")
	if url == "" {
		t.Skip("DBTOOLS_TEST_MYSQL_URL not set, skipping integration test")
	}
	db, err := Open(url)
	if err != nil {
		t.Fatalf("Open() returned error: %v", err)
	}
	t.Cleanup(func() {
		db.Exec(`DROP TABLE IF EXISTS dbtools_migration_history`)
		db.Close()
	})
	return db
}

func TestEnsureSchema_Idempotent(t *testing.T) {
	db := openTestDB(t)
	store := mysqlLedgerStore{}
	if err := store.ensureSchema(db, "dbtools_migration_history"); err != nil {
		t.Fatalf("ensureSchema() returned error: %v", err)
	}
	if err := store.ensureSchema(db, "dbtools_migration_history"); err != nil {
		t.Fatalf("second ensureSchema() returned error: %v", err)
	}
}

func TestSetStatusInsertsAndUpdates(t *testing.T) {
	db := openTestDB(t)
	store := mysqlLedgerStore{}
	if err := store.ensureSchema(db, "dbtools_migration_history"); err != nil {
		t.Fatal(err)
	}

	if err := store.SetStatus(db, 1, ledger.StatusApplied, "first", "dbtools_migration_history"); err != nil {
		t.Fatalf("SetStatus(insert) returned error: %v", err)
	}
	entries, err := store.List(db, "dbtools_migration_history")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Status != ledger.StatusApplied || entries[0].RecordedAt == nil {
		t.Fatalf("after insert: entries = %+v, want one applied row with non-nil RecordedAt", entries)
	}

	if err := store.SetStatus(db, 1, ledger.StatusReverted, "second", "dbtools_migration_history"); err != nil {
		t.Fatalf("SetStatus(update) returned error: %v", err)
	}
	entries, err = store.List(db, "dbtools_migration_history")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Status != ledger.StatusReverted || entries[0].Note != "second" {
		t.Fatalf("after update: entries = %+v, want one reverted row noted 'second'", entries)
	}
}

func TestSetStatusWithHashPreservesHashOnPlainUpdate(t *testing.T) {
	db := openTestDB(t)
	store := mysqlLedgerStore{}
	if err := store.ensureSchema(db, "dbtools_migration_history"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetStatusWithHash(db, 1, ledger.StatusApplied, "with hash", "deadbeef", "dbtools_migration_history"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetStatus(db, 1, ledger.StatusApplied, "touched again", "dbtools_migration_history"); err != nil {
		t.Fatal(err)
	}
	entries, err := store.List(db, "dbtools_migration_history")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].ContentSHA256 != "deadbeef" {
		t.Fatalf("entries = %+v, want content_sha256 preserved across a plain SetStatus", entries)
	}
}

func TestSetStatusAdopted(t *testing.T) {
	db := openTestDB(t)
	store := mysqlLedgerStore{}
	if err := store.ensureSchema(db, "dbtools_migration_history"); err != nil {
		t.Fatal(err)
	}

	if err := store.SetStatusAdopted(db, 1, "adopted from schema_migrations", "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", "dbtools_migration_history"); err != nil {
		t.Fatalf("SetStatusAdopted() returned error: %v", err)
	}
	entries, err := store.List(db, "dbtools_migration_history")
	if err != nil {
		t.Fatal(err)
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
	db := openTestDB(t)
	// Simulate a table created before hash_source existed.
	if _, err := db.Exec(`
CREATE TABLE dbtools_migration_history (
    version     BIGINT       NOT NULL PRIMARY KEY,
    status      VARCHAR(10)  NOT NULL,
    recorded_at DATETIME     NULL,
    note        VARCHAR(400) NULL,
    CHECK (status IN ('applied', 'reverted'))
) ENGINE=InnoDB`); err != nil {
		t.Fatal(err)
	}

	store := mysqlLedgerStore{}
	if err := store.ensureSchema(db, "dbtools_migration_history"); err != nil {
		t.Fatalf("ensureSchema() on preexisting table returned error: %v", err)
	}
	if err := store.SetStatusAdopted(db, 1, "adopted", "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", "dbtools_migration_history"); err != nil {
		t.Fatalf("SetStatusAdopted() after ensureSchema migration returned error: %v", err)
	}
}

func TestAppliedVersions(t *testing.T) {
	db := openTestDB(t)
	store := mysqlLedgerStore{}
	if err := store.ensureSchema(db, "dbtools_migration_history"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetStatus(db, 1, ledger.StatusApplied, "", "dbtools_migration_history"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetStatus(db, 2, ledger.StatusReverted, "", "dbtools_migration_history"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetStatus(db, 3, ledger.StatusApplied, "", "dbtools_migration_history"); err != nil {
		t.Fatal(err)
	}
	versions, err := store.AppliedVersions(db, "dbtools_migration_history")
	if err != nil {
		t.Fatal(err)
	}
	want := []uint64{1, 3}
	if len(versions) != len(want) || versions[0] != want[0] || versions[1] != want[1] {
		t.Errorf("AppliedVersions() = %v, want %v", versions, want)
	}
}
