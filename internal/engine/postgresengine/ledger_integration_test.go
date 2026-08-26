//go:build integration

package postgresengine

import (
	"database/sql"
	"os"
	"testing"

	"github.com/seanpham99/dbtools/internal/ledger"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("DBTOOLS_TEST_POSTGRES_URL")
	if url == "" {
		t.Skip("DBTOOLS_TEST_POSTGRES_URL not set, skipping integration test")
	}
	db, err := Postgres{}.Open(url)
	if err != nil {
		t.Fatalf("Open() returned error: %v", err)
	}
	t.Cleanup(func() {
		db.Exec(`DROP TABLE IF EXISTS dbtools_migration_history`)
		db.Close()
	})
	return db
}

func TestSetStatusAdopted(t *testing.T) {
	db := openTestDB(t)
	store := ledgerStore{}
	if err := store.ensureSchema(db, "dbtools_migration_history"); err != nil {
		t.Fatal(err)
	}

	if err := store.SetStatusAdopted(db, 1, "adopted from schema_migrations", "abc123", "dbtools_migration_history"); err != nil {
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
	if e.ContentSHA256 != "abc123" {
		t.Errorf("ContentSHA256 = %q, want abc123", e.ContentSHA256)
	}
}

func TestEnsureSchema_AddsHashSourceToPreexistingTable(t *testing.T) {
	db := openTestDB(t)
	// Simulate a table created before hash_source existed.
	if _, err := db.Exec(`
CREATE TABLE dbtools_migration_history (
    version         BIGINT       NOT NULL PRIMARY KEY,
    status          VARCHAR(10)  NOT NULL CHECK (status IN ('applied', 'reverted')),
    recorded_at     TIMESTAMPTZ  NULL,
    note            VARCHAR(400) NULL
)`); err != nil {
		t.Fatal(err)
	}

	store := ledgerStore{}
	if err := store.ensureSchema(db, "dbtools_migration_history"); err != nil {
		t.Fatalf("ensureSchema() on preexisting table returned error: %v", err)
	}
	if err := store.SetStatusAdopted(db, 1, "adopted", "abc123", "dbtools_migration_history"); err != nil {
		t.Fatalf("SetStatusAdopted() after ensureSchema migration returned error: %v", err)
	}
}
