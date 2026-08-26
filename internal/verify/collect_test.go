package verify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/engine/sqliteengine"
)

func TestCollect_NoLedgerWalksFilesDirectly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "1_create_widgets.up.sql"),
		[]byte("CREATE TABLE widgets (id INTEGER PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}

	eng := sqliteengine.SQLite{}
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := eng.Open("sqlite://" + dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create the object the migration file describes, but never create
	// dbtools_migration_history — simulating a foreign database.
	if _, err := db.Exec(`CREATE TABLE widgets (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}

	report, err := Collect(db, eng, dir, ".up.sql", "dbtools_migration_history", "local")
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	if len(report.Entries) != 1 {
		t.Fatalf("len(Entries) = %d, want 1", len(report.Entries))
	}
	e := report.Entries[0]
	if e.Status != "OK" {
		t.Errorf("Status = %q, want OK (object exists)", e.Status)
	}
	if !strings.Contains(e.Detail, "no ledger") {
		t.Errorf("Detail = %q, want it to mention no ledger", e.Detail)
	}
}

func TestCollect_NoLedgerReportsDriftForMissingObject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "1_create_widgets.up.sql"),
		[]byte("CREATE TABLE widgets (id INTEGER PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}

	eng := sqliteengine.SQLite{}
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := eng.Open("sqlite://" + dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// widgets is never created — object is missing.

	report, err := Collect(db, eng, dir, ".up.sql", "dbtools_migration_history", "local")
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	if len(report.Entries) != 1 || report.Entries[0].Status != "DRIFT" {
		t.Fatalf("Entries = %+v, want one DRIFT entry", report.Entries)
	}
}

func TestCollect_NoLedgerExcusesDroppedObjects(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "1_create_widgets.up.sql"),
		[]byte("CREATE TABLE widgets (id INTEGER PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2_drop_widgets.up.sql"),
		[]byte("DROP TABLE IF EXISTS widgets;"), 0o644); err != nil {
		t.Fatal(err)
	}

	eng := sqliteengine.SQLite{}
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := eng.Open("sqlite://" + dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// widgets is not in the database (dropped by migration 2).

	report, err := Collect(db, eng, dir, ".up.sql", "dbtools_migration_history", "local")
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	if len(report.Entries) != 2 {
		t.Fatalf("len(Entries) = %d, want 2", len(report.Entries))
	}
	for _, e := range report.Entries {
		if e.Status != "OK" {
			t.Errorf("Entry v%d Status = %q, want OK (dropped object excused)", e.Version, e.Status)
		}
	}
}
