package ledger_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/engine/sqliteengine"
	"github.com/seanpham99/dbtools/internal/migrator"
)

// migratorOpenDB opens the sqlite file directly (like eng.Open would). The
// "sqlite" driver is registered by internal/migrator's sqlite import.
func migratorOpenDB(rawURL string) (*sql.DB, error) {
	p := strings.TrimPrefix(rawURL, "sqlite://")
	return sql.Open("sqlite", p)
}

// TestSync_RefusesDirtyCursor is the C3 regression: when a previous apply
// failed partway, the cursor is dirty and Sync must refuse to backfill —
// otherwise the failed migration silently becomes "applied" in the ledger.
func TestSync_RefusesDirtyCursor(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "1_create_a.up.sql"), []byte("CREATE TABLE ledger_sync_a (id INTEGER PRIMARY KEY);"), 0o644)
	os.WriteFile(filepath.Join(dir, "2_bad.up.sql"), []byte("THIS IS NOT SQL"), 0o644)

	rawURL := "sqlite://" + filepath.Join(dir, "sync.db")
	m, err := migrator.Open(rawURL, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	// Apply version 1, then let version 2 fail — leaving the cursor dirty.
	if _, err := m.Step(); err != nil {
		t.Fatalf("step 1: %v", err)
	}
	if _, err := m.Step(); err == nil {
		t.Fatal("step 2 should fail (bad SQL)")
	}
	v, dirty, _, err := m.Version()
	if err != nil {
		t.Fatal(err)
	}
	if !dirty {
		t.Fatalf("cursor should be dirty after failed apply, got version=%d dirty=%v", v, dirty)
	}

	db, err := migratorOpenDB(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ls := sqliteengine.SQLite{}.Ledger()
	err = ls.Sync(db, m, dir)
	if err == nil {
		t.Fatal("Sync() over a dirty cursor should refuse")
	}
	if !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("Sync() error = %v, want it to mention the dirty cursor", err)
	}

	// And the ledger must NOT have backfilled the failed version.
	entries, err := ls.List(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("ledger has %d entries after refused Sync, want 0 (nothing backfilled over a dirty cursor)", len(entries))
	}
}

func TestSync_BackfillSkipsDirty(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "1_create_a.up.sql"), []byte("CREATE TABLE ledger_sync_skip (id INTEGER PRIMARY KEY);"), 0o644)
	rawURL := "sqlite://" + filepath.Join(dir, "sync.db")
	m, err := migrator.Open(rawURL, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, err := m.Step(); err != nil {
		t.Fatalf("step 1: %v", err)
	}
	db, err := migratorOpenDB(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := (sqliteengine.SQLite{}).Ledger().Sync(db, m, dir); err != nil {
		t.Fatalf("Sync() clean: %v", err)
	}
	entries, err := (sqliteengine.SQLite{}).Ledger().List(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Version != 1 {
		t.Fatalf("ledger = %+v, want exactly version 1 applied", entries)
	}
}
