//go:build integration

package verify

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/seanpham99/dbtools/internal/engine/postgresengine"
	"github.com/seanpham99/dbtools/internal/ledger"
)

const pgLedgerSchema = `
CREATE TABLE IF NOT EXISTS dbtools_migration_history (
    version         BIGINT       NOT NULL PRIMARY KEY,
    status          VARCHAR(10)  NOT NULL CHECK (status IN ('applied', 'reverted')),
    recorded_at     TIMESTAMPTZ  NULL,
    note            VARCHAR(400) NULL,
    content_sha256  CHAR(64)     NULL
);`

func TestCollect_Postgres_DetectsMissingObject(t *testing.T) {
	url := os.Getenv("DBTOOLS_TEST_POSTGRES_URL")
	if url == "" {
		t.Skip("DBTOOLS_TEST_POSTGRES_URL not set, skipping integration test")
	}

	eng := postgresengine.Postgres{}
	db, err := eng.Open(url)
	if err != nil {
		t.Fatalf("eng.Open() returned error: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("db.Ping() returned error: %v", err)
	}

	// Clean tracking and test tables
	db.Exec(`DROP TABLE IF EXISTS dbtools_migration_history`)
	db.Exec(`DROP TABLE IF EXISTS schema_migrations`)
	db.Exec(`DROP TABLE IF EXISTS dbtools_pg_verify_items`)
	defer db.Exec(`DROP TABLE IF EXISTS dbtools_pg_verify_items`)
	defer db.Exec(`DROP TABLE IF EXISTS dbtools_migration_history`)

	dir := t.TempDir()
	sqlContent := []byte("CREATE TABLE dbtools_pg_verify_items (id SERIAL PRIMARY KEY, name TEXT);")
	if err := os.WriteFile(filepath.Join(dir, "20260101000000_create_items.up.sql"), sqlContent, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(pgLedgerSchema); err != nil {
		t.Fatalf("ensuring ledger table: %v", err)
	}
	// Simulate recorded as applied, but table was not created in live DB
	if err := eng.Ledger().SetStatus(db, 20260101000000, ledger.StatusApplied, "missing table simulation"); err != nil {
		t.Fatal(err)
	}

	report, err := Collect(db, eng, dir, "pg-target")
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	if len(report.Entries) != 1 || report.Entries[0].Status != "DRIFT" {
		t.Fatalf("Collect() = %+v, want one DRIFT entry for missing table", report.Entries)
	}

	// Create table and verify it turns OK
	if _, err := db.Exec(string(sqlContent)); err != nil {
		t.Fatal(err)
	}

	report, err = Collect(db, eng, dir, "pg-target")
	if err != nil {
		t.Fatalf("second Collect() returned error: %v", err)
	}
	if len(report.Entries) != 1 || report.Entries[0].Status != "OK" {
		t.Fatalf("second Collect() = %+v, want one OK entry", report.Entries)
	}
}

func TestCollect_Postgres_DetectsContentHashDrift(t *testing.T) {
	url := os.Getenv("DBTOOLS_TEST_POSTGRES_URL")
	if url == "" {
		t.Skip("DBTOOLS_TEST_POSTGRES_URL not set, skipping integration test")
	}

	eng := postgresengine.Postgres{}
	db, err := eng.Open(url)
	if err != nil {
		t.Fatalf("eng.Open() returned error: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("db.Ping() returned error: %v", err)
	}

	db.Exec(`DROP TABLE IF EXISTS dbtools_migration_history`)
	db.Exec(`DROP TABLE IF EXISTS schema_migrations`)
	db.Exec(`DROP TABLE IF EXISTS dbtools_pg_hash_table`)
	defer db.Exec(`DROP TABLE IF EXISTS dbtools_pg_hash_table`)
	defer db.Exec(`DROP TABLE IF EXISTS dbtools_migration_history`)

	dir := t.TempDir()
	originalContent := []byte("CREATE TABLE dbtools_pg_hash_table (id INT PRIMARY KEY);")
	if err := os.WriteFile(filepath.Join(dir, "20260101000000_create_hash_table.up.sql"), originalContent, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(pgLedgerSchema); err != nil {
		t.Fatalf("ensuring ledger table: %v", err)
	}
	if _, err := db.Exec(string(originalContent)); err != nil {
		t.Fatal(err)
	}

	// Record hash of original content in ledger
	origSum := sha256.Sum256(originalContent)
	origHash := hex.EncodeToString(origSum[:])
	if err := eng.Ledger().SetStatusWithHash(db, 20260101000000, ledger.StatusApplied, "hash test", origHash); err != nil {
		t.Fatal(err)
	}

	// Verify reports OK initially
	report, err := Collect(db, eng, dir, "pg-target")
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	if len(report.Entries) != 1 || report.Entries[0].Status != "OK" {
		t.Fatalf("Collect() = %+v, want OK", report.Entries)
	}

	// Now modify the file on disk (post-apply tampering)
	modifiedContent := []byte("CREATE TABLE dbtools_pg_hash_table (id INT PRIMARY KEY, modified_col TEXT);")
	if err := os.WriteFile(filepath.Join(dir, "20260101000000_create_hash_table.up.sql"), modifiedContent, 0o644); err != nil {
		t.Fatal(err)
	}

	// Collect should flag DRIFT due to file hash mismatch
	report, err = Collect(db, eng, dir, "pg-target")
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	if len(report.Entries) != 1 || report.Entries[0].Status != "DRIFT" {
		t.Fatalf("Collect() = %+v, want DRIFT on file hash mismatch", report.Entries)
	}
}
