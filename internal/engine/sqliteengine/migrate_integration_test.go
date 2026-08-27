//go:build integration

package sqliteengine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/seanpham99/dbtools/internal/migrator"
)

// TestLiveMigrateUpAndLedger exercises the full golang-migrate path and
// the ledger round trip against a real SQLite file — the shared migration
// machinery (migrator.Open, Up, Version, ledger.Sync/SetStatus/List) that
// the MSSQL/Postgres engines already cover in their own integration tests
// but SQLite had none for.
func TestLiveMigrateUpAndLedger(t *testing.T) {
	rawURL := os.Getenv("DBTOOLS_TEST_SQLITE_URL")
	if rawURL == "" {
		t.Skip("DBTOOLS_TEST_SQLITE_URL not set, skipping integration test")
	}

	// Isolate each run: the URL names a file path (sqlite:///tmp/...);
	// drop any file left by a previous run so this test starts blank.
	if p := PathFromURL(rawURL); p != "" {
		for _, sidecar := range []string{p, p + "-wal", p + "-shm"} {
			os.Remove(sidecar)
		}
	}

	eng := SQLite{}
	db, err := eng.Open(rawURL)
	if err != nil {
		t.Fatalf("Open() returned error: %v", err)
	}
	defer db.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "20260817000001_widgets.up.sql"),
		[]byte("CREATE TABLE it_widgets (id INTEGER PRIMARY KEY, label TEXT NOT NULL);"), 0o644); err != nil {
		t.Fatal(err)
	}

	d, err := migrator.ReadDir(dir, ".up.sql")
	if err != nil {
		t.Fatal(err)
	}
	runner := migrator.NewRunner(SQLite{}, db, d, "dbtools_sqlite_migrate_it_history")

	appliedCount, err := runner.Up(context.Background())
	if err != nil {
		t.Fatalf("Up() returned error: %v", err)
	}
	if appliedCount == 0 {
		t.Fatal("Up() applied 0 migrations, want at least one")
	}
	state, err := runner.State(context.Background())
	if err != nil || state.Dirty || !state.HasVersion || state.Version != 20260817000001 {
		t.Fatalf("State() = %+v err=%v", state, err)
	}

	// Ledger round trip through the engine seam.
	store := ledgerStore{}
	if err := store.EnsureSchema(db, "dbtools_sqlite_migrate_it_history"); err != nil {
		t.Fatalf("EnsureSchema() returned error: %v", err)
	}
	if err := store.SetStatus(db, 20260817000001, "applied", "applied via integration test", "dbtools_sqlite_migrate_it_history"); err != nil {
		t.Fatalf("SetStatus() returned error: %v", err)
	}
	entries, err := store.List(db, "dbtools_sqlite_migrate_it_history")
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if len(entries) != 1 || entries[0].Version != 20260817000001 {
		t.Fatalf("List() = %+v, want one row for version 20260817000001", entries)
	}

	// The migration's table must exist, proving the migration SQL ran.
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='it_widgets'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("it_widgets table count = %d, want 1", n)
	}

	// A second run must find nothing pending: the ledger is now the only
	// record, so "already applied" is derived from the rows just written.
	again, err := runner.Up(context.Background())
	if err != nil {
		t.Fatalf("second Up() returned error: %v", err)
	}
	if again != 0 {
		t.Fatalf("second Up() applied %d migrations, want 0", again)
	}
}

// TestLiveMigrateRollbackOnBadSQL guards the shared migrate path against a
// regression where a failing migration would leave the cursor dirty and a
// subsequent run would replay earlier migrations.
func TestLiveMigrateDirtyFlag(t *testing.T) {
	rawURL := os.Getenv("DBTOOLS_TEST_SQLITE_URL")
	if rawURL == "" {
		t.Skip("DBTOOLS_TEST_SQLITE_URL not set, skipping integration test")
	}
	if p := PathFromURL(rawURL); p != "" {
		for _, sidecar := range []string{p, p + "-wal", p + "-shm"} {
			os.Remove(sidecar)
		}
	}

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "1_good.up.sql"), []byte("CREATE TABLE good_t (id INTEGER);"), 0o644)
	os.WriteFile(filepath.Join(dir, "2_bad.up.sql"), []byte("THIS IS NOT SQL"), 0o644)

	eng := SQLite{}
	db, err := eng.Open(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	d, err := migrator.ReadDir(dir, ".up.sql")
	if err != nil {
		t.Fatal(err)
	}
	runner := migrator.NewRunner(eng, db, d, "dbtools_sqlite_migrate_it_history")

	applied, err := runner.Up(context.Background())
	if err == nil {
		t.Fatal("Up() with a bad second migration should error")
	}
	if applied != 1 {
		t.Fatalf("Up() applied %d migrations before failing, want 1 (the good one)", applied)
	}

	// The failed migration's "applying" row survives, which is what stops
	// the next run rather than a separate dirty flag — and unlike a
	// boolean, it names the migration that died.
	state, err := runner.State(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !state.Dirty || state.Applying != 2 {
		t.Fatalf("after failed Up(): state = %+v, want dirty at version 2", state)
	}

	// A second run must refuse rather than apply more SQL on top of a
	// schema in an unknown state.
	if _, err := runner.Up(context.Background()); err == nil {
		t.Fatal("second Up() succeeded over a mid-apply migration, want a refusal")
	}
}
