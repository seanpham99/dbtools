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
