//go:build integration

package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dbtools/dbtools/internal/config"
	"github.com/dbtools/dbtools/internal/dbconn"
	"github.com/dbtools/dbtools/internal/ledger"
	"github.com/dbtools/dbtools/internal/testdb"
)

func TestRun_AppliesMigrations(t *testing.T) {
	url := os.Getenv("DBTOOLS_TEST_MSSQL_URL")
	if url == "" {
		t.Skip("DBTOOLS_TEST_MSSQL_URL not set, skipping integration test")
	}
	t.Setenv("DBTOOLS_APPLY_TEST_URL", url)

	// Defensive cleanup: another task's integration test may have left
	// schema_migrations in an unexpected state. Drop it so this test
	// behaves as if against a fresh database, regardless of run order.
	if err := testdb.ResetTracking(url); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "20260101000000_create_widgets.up.sql"),
		[]byte("CREATE TABLE dbtools_test_apply (id INT PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		MigrationsDir: dir,
		Targets:       map[string]config.Target{"local": {URLEnv: "DBTOOLS_APPLY_TEST_URL"}},
	}

	status, err := Run(cfg, "local")
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}
	if len(status.Pending) != 0 {
		t.Errorf("expected no pending migrations after Run(), got %v", status.Pending)
	}
	if !status.HasVersion || status.CurrentVersion != 20260101000000 {
		t.Errorf("unexpected status after Run(): %+v", status)
	}
}

func TestRun_RecordsLedgerEntries(t *testing.T) {
	url := os.Getenv("DBTOOLS_TEST_MSSQL_URL")
	if url == "" {
		t.Skip("DBTOOLS_TEST_MSSQL_URL not set, skipping integration test")
	}
	t.Setenv("DBTOOLS_APPLY_LEDGER_TEST_URL", url)

	if err := testdb.ResetTracking(url); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "20260101000000_create_widgets.up.sql"),
		[]byte("CREATE TABLE dbtools_test_apply_ledger (id INT PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		MigrationsDir: dir,
		Targets:       map[string]config.Target{"local": {URLEnv: "DBTOOLS_APPLY_LEDGER_TEST_URL"}},
	}

	if _, err := Run(cfg, "local"); err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	db, err := dbconn.Open(url)
	if err != nil {
		t.Fatalf("dbconn.Open() returned error: %v", err)
	}
	defer db.Close()

	entries, err := ledger.List(db)
	if err != nil {
		t.Fatalf("ledger.List() returned error: %v", err)
	}
	if len(entries) != 1 || entries[0].Version != 20260101000000 || entries[0].Status != ledger.StatusApplied {
		t.Fatalf("ledger.List() = %+v, want one applied row for version 20260101000000", entries)
	}
	if entries[0].RecordedAt == nil {
		t.Error("entries[0].RecordedAt = nil, want non-nil for a freshly-applied migration")
	}
}
