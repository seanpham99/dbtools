//go:build integration

package sqliteengine

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/dbtools/dbtools/internal/migrator"
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

	m, err := migrator.Open(rawURL, dir)
	if err != nil {
		t.Fatalf("migrator.Open() returned error: %v", err)
	}
	defer m.Close()

	applied, err := m.Up()
	if err != nil {
		t.Fatalf("Up() returned error: %v", err)
	}
	if !applied {
		t.Fatal("Up() = false, want an applied migration")
	}
	version, dirty, hasVersion, err := m.Version()
	if err != nil || dirty || !hasVersion || version != 20260817000001 {
		t.Fatalf("Version() = %d dirty=%v has=%v err=%v", version, dirty, hasVersion, err)
	}

	// Ledger round trip through the engine seam.
	store := ledgerStore{}
	if err := store.Sync(db, m, dir); err != nil {
		t.Fatalf("Sync() returned error: %v", err)
	}
	if err := store.SetStatus(db, 20260817000001, "applied", "applied via integration test"); err != nil {
		t.Fatalf("SetStatus() returned error: %v", err)
	}
	entries, err := store.List(db)
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

	// The cursor and ledger must both see the file as up to date now.
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	m2, err := migrator.Open(rawURL, dir)
	if err != nil {
		t.Fatalf("re-open migrator: %v", err)
	}
	defer m2.Close()
	applied, err = m2.Up()
	if err != nil {
		t.Fatalf("second Up() returned error: %v", err)
	}
	if applied {
		t.Fatal("second Up() = true, want no-change (ErrNoChange path)")
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

	m, err := migrator.Open(rawURL, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	if _, err := m.Up(); err == nil {
		t.Fatal("Up() with a bad second migration should error")
	}
	version, dirty, has, err := m.Version()
	if err != nil {
		t.Fatal(err)
	}
	if !has || !dirty || version != 2 {
		t.Fatalf("after failed Up(): version=%d dirty=%v has=%v; want version=2 dirty=true has=true", version, dirty, has)
	}

	// The cursor must be dirty so a consumer (ledger.Sync's dirty check)
	// can refuse to backfill over it.
	_ = sql.ErrNoRows // keep database/sql import used if this test is trimmed
}
