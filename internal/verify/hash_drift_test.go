package verify

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/engine"
	_ "github.com/seanpham99/dbtools/internal/engine/sqliteengine"
	"github.com/seanpham99/dbtools/internal/ledger"
)

// TestCollect_DetectsEditedMigrationAfterApply is the R2 regression: a
// migration file edited after it was applied (the most common real drift)
// must be reported even when every object still exists. The hash is
// recorded at apply time; a different file hash is DRIFT.
func TestCollect_DetectsEditedMigrationAfterApply(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "20260101000000_create_widgets.up.sql")
	if err := os.WriteFile(file, []byte("CREATE TABLE dbtools_test_hash_drift (id INTEGER PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}

	rawURL := "sqlite://" + filepath.Join(dir, "verify.db")
	eng, err := engine.ForTarget("", rawURL)
	if err != nil {
		t.Fatal(err)
	}
	db, err := eng.Open(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create the ledger table (as apply.Run's Sync would).
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS dbtools_migration_history (
		version INTEGER NOT NULL PRIMARY KEY,
		status TEXT NOT NULL CHECK (status IN ('applied', 'reverted')),
		recorded_at TIMESTAMP NULL,
		note TEXT NULL,
		content_sha256 TEXT NULL,
		hash_source TEXT NULL)`); err != nil {
		t.Fatalf("creating ledger: %v", err)
	}

	// Simulate an apply: record the migration as applied WITH its current
	// file hash.
	hash, err := hashOf(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Ledger().SetStatusWithHash(db, 20260101000000, ledger.StatusApplied, "applied via up/push", hash, "dbtools_migration_history"); err != nil {
		t.Fatal(err)
	}
	// The migration's object actually exists (it was really applied).
	if _, err := db.Exec(`CREATE TABLE dbtools_test_hash_drift (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("creating table: %v", err)
	}
	defer db.Exec(`DROP TABLE dbtools_test_hash_drift`)

	// Verify clean before any edit.
	report, err := Collect(db, eng, dir, ".up.sql", "dbtools_migration_history", "test-target")
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	if len(report.Entries) != 1 || report.Entries[0].Status != "OK" {
		t.Fatalf("Collect() before edit = %+v, want one OK entry", report.Entries)
	}

	// Edit the applied migration — the classic drift: an ALTER added by
	// hand to a file that already ran against prod.
	if err := os.WriteFile(file, []byte("CREATE TABLE dbtools_test_hash_drift (id INTEGER PRIMARY KEY);\nALTER TABLE dbtools_test_hash_drift ADD COLUMN extra TEXT;"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err = Collect(db, eng, dir, ".up.sql", "dbtools_migration_history", "test-target")
	if err != nil {
		t.Fatalf("Collect() after edit returned error: %v", err)
	}
	if len(report.Entries) != 1 || report.Entries[0].Status != "DRIFT" {
		t.Fatalf("Collect() after edit = %+v, want one DRIFT entry", report.Entries)
	}
	if !strings.Contains(report.Entries[0].Detail, "edited after it was applied") {
		t.Fatalf("DRIFT detail = %q, want it to mention the file was edited after apply", report.Entries[0].Detail)
	}
}

func hashOf(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// TestCollect_BackfilledRowsWithoutHashAreNotDrift: rows recorded before
// content hashing existed (empty hash) must not be reported as drift on
// their own — there is no baseline to compare against.
func TestCollect_BackfilledRowsWithoutHashAreNotDrift(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "20260101000000_create_widgets.up.sql")
	if err := os.WriteFile(file, []byte("CREATE TABLE dbtools_test_hash_backfill (id INTEGER PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}

	rawURL := "sqlite://" + filepath.Join(dir, "verify.db")
	eng, err := engine.ForTarget("", rawURL)
	if err != nil {
		t.Fatal(err)
	}
	db, err := eng.Open(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS dbtools_migration_history (
		version INTEGER NOT NULL PRIMARY KEY,
		status TEXT NOT NULL CHECK (status IN ('applied', 'reverted')),
		recorded_at TIMESTAMP NULL,
		note TEXT NULL,
		content_sha256 TEXT NULL,
		hash_source TEXT NULL)`); err != nil {
		t.Fatalf("creating ledger: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE dbtools_test_hash_backfill (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("creating table: %v", err)
	}
	defer db.Exec(`DROP TABLE dbtools_test_hash_backfill`)
	if err := eng.Ledger().SetStatus(db, 20260101000000, ledger.StatusApplied, "backfilled: applied before ledger existed", "dbtools_migration_history"); err != nil {
		t.Fatal(err)
	}

	report, err := Collect(db, eng, dir, ".up.sql", "dbtools_migration_history", "test-target")
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	if len(report.Entries) != 1 || report.Entries[0].Status != "OK" {
		t.Fatalf("Collect() = %+v, want one OK entry (no hash baseline)", report.Entries)
	}
}
