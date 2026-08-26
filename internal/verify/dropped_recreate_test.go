package verify

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seanpham99/dbtools/internal/engine"
	_ "github.com/seanpham99/dbtools/internal/engine/sqliteengine"
	"github.com/seanpham99/dbtools/internal/ledger"
)

// TestCollect_DropThenRecreateIsNotPermanentDrift is the review's §3
// dropped-set regression: v1 drops widgets, v9 re-creates them. A later
// genuine disappearance of widgets must be reported as DRIFT — not excused
// by the stale "dropped at v1" fact.
func TestCollect_DropThenRecreateIsNotPermanentDrift(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "1_drop_widgets.up.sql"), []byte("DROP TABLE IF EXISTS dbtools_test_recreate_widgets;"), 0o644)
	os.WriteFile(filepath.Join(dir, "2_create_widgets.up.sql"), []byte("CREATE TABLE dbtools_test_recreate_widgets (id INTEGER PRIMARY KEY);"), 0o644)

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
		content_sha256 TEXT NULL)`); err != nil {
		t.Fatal(err)
	}
	// v1 drops (recorded), v2 re-creates (recorded) and the object exists.
	if _, err := db.Exec(`CREATE TABLE dbtools_test_recreate_widgets (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DROP TABLE dbtools_test_recreate_widgets`)

	if err := eng.Ledger().SetStatus(db, 1, ledger.StatusApplied, "drops widgets"); err != nil {
		t.Fatal(err)
	}
	if err := eng.Ledger().SetStatus(db, 2, ledger.StatusApplied, "re-creates widgets"); err != nil {
		t.Fatal(err)
	}

	report, err := Collect(db, eng, dir, ".up.sql", "test-target")
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	for _, e := range report.Entries {
		if e.Status != "OK" {
			t.Errorf("entry %d = %s (%s), want OK when the object exists after re-create", e.Version, e.Status, e.Detail)
		}
	}

	// Now the object genuinely disappears — with the old global unordered
	// dropped-set this was reported OK forever; it must now be DRIFT.
	if _, err := db.Exec(`DROP TABLE dbtools_test_recreate_widgets`); err != nil {
		t.Fatal(err)
	}
	report, err = Collect(db, eng, dir, ".up.sql", "test-target")
	if err != nil {
		t.Fatalf("second Collect() returned error: %v", err)
	}
	found := false
	for _, e := range report.Entries {
		if e.Version == 2 && e.Status == "DRIFT" {
			found = true
		}
	}
	if !found {
		t.Fatalf("Collect() after genuine drop = %+v, want v2 (the re-create) reported DRIFT", report.Entries)
	}
}

func TestCollect_SQLite_CreatedThenDroppedByLaterMigrationIsNotDrift(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "1_create_widgets.up.sql"), []byte("CREATE TABLE dbtools_test_dropped (id INTEGER PRIMARY KEY);"), 0o644)
	os.WriteFile(filepath.Join(dir, "2_drop_widgets.up.sql"), []byte("DROP TABLE IF EXISTS dbtools_test_dropped;"), 0o644)

	rawURL := "sqlite://" + filepath.Join(dir, "verify_drop.db")
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
		content_sha256 TEXT NULL)`); err != nil {
		t.Fatal(err)
	}

	if err := eng.Ledger().SetStatus(db, 1, ledger.StatusApplied, "creates table"); err != nil {
		t.Fatal(err)
	}
	if err := eng.Ledger().SetStatus(db, 2, ledger.StatusApplied, "drops table"); err != nil {
		t.Fatal(err)
	}

	report, err := Collect(db, eng, dir, ".up.sql", "test-target")
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	if len(report.Entries) != 2 {
		t.Fatalf("Collect() returned %d entries, want 2", len(report.Entries))
	}
	for _, e := range report.Entries {
		if e.Status != "OK" {
			t.Errorf("entry %d = %s (%s), want OK when dropped by later migration", e.Version, e.Status, e.Detail)
		}
	}
}
