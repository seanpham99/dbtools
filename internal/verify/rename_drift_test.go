package verify

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/engine"
	_ "github.com/seanpham99/dbtools/internal/engine/sqliteengine"
	"github.com/seanpham99/dbtools/internal/ledger"
)

// rebuildMigration is the shape every alpha-lab constraint migration uses:
// create the constrained table under a temporary name, copy, drop the old
// one, rename the temporary table into place. The temporary name never
// survives the file, so requiring it to exist is a false DRIFT.
const rebuildMigration = `CREATE TABLE dbtools_test_widget_new (
    id TEXT PRIMARY KEY,
    n INTEGER NOT NULL CHECK (n >= 0)
);
INSERT INTO dbtools_test_widget_new (id, n) SELECT id, 0 FROM dbtools_test_widget;
DROP TABLE dbtools_test_widget;
ALTER TABLE dbtools_test_widget_new RENAME TO dbtools_test_widget;`

// rebuildTarget opens a sqlite database that already holds the schema the
// rebuild migration describes: dbtools_test_widget, with the CHECK applied
// and the temporary name absent.
func rebuildTarget(t *testing.T, dir string) (engine.Engine, *sql.DB) {
	t.Helper()
	rawURL := "sqlite://" + filepath.Join(dir, "verify.db")
	eng, err := engine.ForTarget("", rawURL)
	if err != nil {
		t.Fatal(err)
	}
	db, err := eng.Open(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`CREATE TABLE dbtools_migration_history (
		version INTEGER NOT NULL PRIMARY KEY,
		status TEXT NOT NULL CHECK (status IN ('applied', 'reverted')),
		recorded_at TIMESTAMP NULL,
		note TEXT NULL,
		content_sha256 TEXT NULL,
		hash_source TEXT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE dbtools_test_widget (
		id TEXT PRIMARY KEY,
		n INTEGER NOT NULL CHECK (n >= 0))`); err != nil {
		t.Fatal(err)
	}
	return eng, db
}

func TestCollect_RebuildInPlaceRenameIsNotDrift(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "1_base.up.sql"),
		[]byte("CREATE TABLE dbtools_test_widget (id TEXT PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2_rebuild.up.sql"),
		[]byte(rebuildMigration), 0o644); err != nil {
		t.Fatal(err)
	}

	eng, db := rebuildTarget(t, dir)
	for _, v := range []uint64{1, 2} {
		if err := eng.Ledger().SetStatus(db, v, ledger.StatusApplied, "", "dbtools_migration_history"); err != nil {
			t.Fatal(err)
		}
	}

	report, err := Collect(db, eng, dir, ".up.sql", "dbtools_migration_history", "test-target")
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	for _, e := range report.Entries {
		if e.Status != "OK" {
			t.Errorf("version %d = %s (%s), want OK — the temporary name is renamed away by the same file", e.Version, e.Status, e.Detail)
		}
	}
}

func TestCollect_CreateAndDropInOneFileIsNotDrift(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "1_scratch.up.sql"),
		[]byte("CREATE TABLE dbtools_test_widget_scratch (id INTEGER PRIMARY KEY);\nDROP TABLE dbtools_test_widget_scratch;"), 0o644); err != nil {
		t.Fatal(err)
	}

	eng, db := rebuildTarget(t, dir)
	if err := eng.Ledger().SetStatus(db, 1, ledger.StatusApplied, "", "dbtools_migration_history"); err != nil {
		t.Fatal(err)
	}

	report, err := Collect(db, eng, dir, ".up.sql", "dbtools_migration_history", "test-target")
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	if len(report.Entries) != 1 {
		t.Fatalf("len(Entries) = %d, want 1", len(report.Entries))
	}
	if e := report.Entries[0]; e.Status != "OK" {
		t.Errorf("status = %s (%s), want OK — the file creates and drops the object itself", e.Status, e.Detail)
	}
}

// TestCollect_MissingObjectIsStillDrift is the guard on the fix: excusing
// what a file destroys must not excuse an object that is genuinely gone.
func TestCollect_MissingObjectIsStillDrift(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "1_widgets.up.sql"),
		[]byte("CREATE TABLE dbtools_test_widget (id TEXT PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2_rebuild.up.sql"),
		[]byte(rebuildMigration), 0o644); err != nil {
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

	if _, err := db.Exec(`CREATE TABLE dbtools_migration_history (
		version INTEGER NOT NULL PRIMARY KEY,
		status TEXT NOT NULL CHECK (status IN ('applied', 'reverted')),
		recorded_at TIMESTAMP NULL,
		note TEXT NULL,
		content_sha256 TEXT NULL,
		hash_source TEXT NULL)`); err != nil {
		t.Fatal(err)
	}
	// No widget table at all: version 2 removes the name version 1 created
	// and its rename never produced the replacement.
	for _, v := range []uint64{1, 2} {
		if err := eng.Ledger().SetStatus(db, v, ledger.StatusApplied, "", "dbtools_migration_history"); err != nil {
			t.Fatal(err)
		}
	}

	report, err := Collect(db, eng, dir, ".up.sql", "dbtools_migration_history", "test-target")
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	var drifted []Entry
	for _, e := range report.Entries {
		if e.Status != "OK" {
			drifted = append(drifted, e)
		}
	}
	// Version 2 owns the name once it drops it, so its absence is reported
	// against version 2. Version 1 stays excused by the cross-version drop
	// rule that predates this fix.
	if len(drifted) != 1 {
		t.Fatalf("drift entries = %+v, want exactly version 2 reporting the missing name", drifted)
	}
	if drifted[0].Version != 2 {
		t.Errorf("drift reported against version %d, want 2 — the version that removed the name", drifted[0].Version)
	}
	if !strings.Contains(drifted[0].Detail, "dbtools_test_widget:") {
		t.Errorf("drift detail = %q, want the final name reported missing", drifted[0].Detail)
	}
}

// TestCollect_RenameTargetIsChecked covers the other half of the fix: the
// name a rename produces must actually exist, so a rename that never ran is
// caught even though the temporary name is excused.
func TestCollect_RenameTargetIsChecked(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "1_base.up.sql"),
		[]byte("CREATE TABLE dbtools_test_widget (id TEXT PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2_rebuild.up.sql"),
		[]byte(rebuildMigration), 0o644); err != nil {
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

	if _, err := db.Exec(`CREATE TABLE dbtools_migration_history (
		version INTEGER NOT NULL PRIMARY KEY,
		status TEXT NOT NULL CHECK (status IN ('applied', 'reverted')),
		recorded_at TIMESTAMP NULL,
		note TEXT NULL,
		content_sha256 TEXT NULL,
		hash_source TEXT NULL)`); err != nil {
		t.Fatal(err)
	}
	// Only the temporary name survives: the rename did not run.
	if _, err := db.Exec(`CREATE TABLE dbtools_test_widget_new (id TEXT PRIMARY KEY, n INTEGER NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE dbtools_test_widget (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DROP TABLE dbtools_test_widget`); err != nil {
		t.Fatal(err)
	}
	for _, v := range []uint64{1, 2} {
		if err := eng.Ledger().SetStatus(db, v, ledger.StatusApplied, "", "dbtools_migration_history"); err != nil {
			t.Fatal(err)
		}
	}

	report, err := Collect(db, eng, dir, ".up.sql", "dbtools_migration_history", "test-target")
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	found := false
	for _, e := range report.Entries {
		if e.Version == 2 && strings.Contains(e.Detail, "dbtools_test_widget:") {
			found = true
		}
	}
	if !found {
		t.Fatalf("entries = %+v, want version 2 to report the rename target missing", report.Entries)
	}
}

// TestCollect_DropThenCreateStillChecksTheName pins the ordering rule at
// the level a user sees it. A migration that DROPs a table and then CREATEs
// it back under the same name leaves that name in place, so its absence is
// real drift. Reading the file without order — create and drop both present
// — would excuse it and report nothing.
func TestCollect_DropThenCreateStillChecksTheName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "1_base.up.sql"),
		[]byte("CREATE TABLE dbtools_test_widget (id TEXT PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2_reshape.up.sql"),
		[]byte("DROP TABLE dbtools_test_widget;\nCREATE TABLE dbtools_test_widget (id TEXT PRIMARY KEY, n INTEGER);\n"), 0o644); err != nil {
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

	if _, err := db.Exec(`CREATE TABLE dbtools_migration_history (
		version INTEGER NOT NULL PRIMARY KEY,
		status TEXT NOT NULL CHECK (status IN ('applied', 'reverted')),
		recorded_at TIMESTAMP NULL,
		note TEXT NULL,
		content_sha256 TEXT NULL,
		hash_source TEXT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, v := range []uint64{1, 2} {
		if err := eng.Ledger().SetStatus(db, v, ledger.StatusApplied, "", "dbtools_migration_history"); err != nil {
			t.Fatal(err)
		}
	}

	report, err := Collect(db, eng, dir, ".up.sql", "dbtools_migration_history", "test-target")
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	found := false
	for _, e := range report.Entries {
		if e.Status == "DRIFT" && strings.Contains(e.Detail, "dbtools_test_widget:") {
			found = true
		}
	}
	if !found {
		t.Fatalf("entries = %+v, want the drop-then-create name reported missing — the file leaves it in place", report.Entries)
	}
}
