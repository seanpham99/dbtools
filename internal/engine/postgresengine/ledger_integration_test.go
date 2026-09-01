//go:build integration

package postgresengine

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/ledger"
	"github.com/seanpham99/dbtools/internal/logger"
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

// captureLogger redirects the default logger into buf until the test ends.
func captureLogger(t *testing.T) *strings.Builder {
	t.Helper()
	var buf strings.Builder
	logger.SetOutput(&buf)
	t.Cleanup(func() { logger.SetOutput(os.Stderr) })
	return &buf
}

func TestSetStatusAdopted(t *testing.T) {
	db := openTestDB(t)
	store := ledgerStore{}
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
	if err := store.SetStatusAdopted(db, 1, "adopted", "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08", "dbtools_migration_history"); err != nil {
		t.Fatalf("SetStatusAdopted() after ensureSchema migration returned error: %v", err)
	}
}

// A steady-state ensureSchema must emit no server notices at all — the
// log is a migration job's only output, and routine "already exists,
// skipping" noise on every command would bury real migration reports.
// Suppression lives in ensureSchema (which simply emits nothing), never
// in the connection-wide notice handler: a migration that deliberately
// RAISEs the same text must still reach the log.
func TestEnsureSchema_EmitsNoNoticesOnSteadyState(t *testing.T) {
	db := openTestDB(t)
	store := ledgerStore{}
	if err := store.ensureSchema(db, "dbtools_migration_history"); err != nil {
		t.Fatal(err)
	}

	logs := captureLogger(t)
	if err := store.ensureSchema(db, "dbtools_migration_history"); err != nil {
		t.Fatalf("second ensureSchema() returned error: %v", err)
	}
	if out := logs.String(); out != "" {
		t.Fatalf("steady-state ensureSchema() logged notices:\n%s", out)
	}
}

// The connection-wide notice handler must not filter by message text: a
// migration that RAISEs exactly the routine-suppression string reaches
// the log.
func TestNoticeHandler_PassesMigrationNoticesThrough(t *testing.T) {
	db := openTestDB(t)

	logs := captureLogger(t)
	if _, err := db.Exec(`DO $$ BEGIN RAISE NOTICE 'already exists, skipping'; END $$;`); err != nil {
		t.Fatalf("RAISE NOTICE exec failed: %v", err)
	}
	if out := logs.String(); !strings.Contains(out, "already exists, skipping") {
		t.Fatalf("migration notice was suppressed, log output:\n%s", out)
	}
}
