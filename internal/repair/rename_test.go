package repair

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seanpham99/dbtools/internal/engine"
	_ "github.com/seanpham99/dbtools/internal/engine/sqliteengine"
	"github.com/seanpham99/dbtools/internal/ledger"
)

// TestRun_RebuildInPlaceRenameNeedsNoForce: a migration that renames its
// temporary table away left a name that no longer exists, so repair used to
// refuse it and demand --force. The schema it describes is present under
// the renamed name, which is what repair must accept.
func TestRun_RebuildInPlaceRenameNeedsNoForce(t *testing.T) {
	dir := t.TempDir()
	migration := `CREATE TABLE dbtools_test_repair_widget_new (
    id TEXT PRIMARY KEY,
    n INTEGER NOT NULL CHECK (n >= 0)
);
DROP TABLE dbtools_test_repair_widget;
ALTER TABLE dbtools_test_repair_widget_new RENAME TO dbtools_test_repair_widget;`
	if err := os.WriteFile(filepath.Join(dir, "20260101000000_rebuild.up.sql"),
		[]byte(migration), 0o644); err != nil {
		t.Fatal(err)
	}

	rawURL := "sqlite://" + filepath.Join(dir, "repair.db")
	eng, err := engine.ForTarget("", rawURL)
	if err != nil {
		t.Fatal(err)
	}
	db, err := eng.Open(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE dbtools_test_repair_widget (
		id TEXT PRIMARY KEY, n INTEGER NOT NULL CHECK (n >= 0))`); err != nil {
		t.Fatal(err)
	}

	pairs := []Pair{{Version: 20260101000000, Status: ledger.StatusApplied}}
	if _, err := run(db, eng, dir, ".up.sql", "dbtools_migration_history", pairs, false); err != nil {
		t.Fatalf("run() = %v, want nil — the rename target exists and the temporary name is renamed away", err)
	}

	// The object a file really did fail to leave behind must still be
	// refused without --force.
	if err := os.WriteFile(filepath.Join(dir, "20260101000001_absent.up.sql"),
		[]byte("CREATE TABLE dbtools_test_repair_absent (id TEXT PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}
	absent := []Pair{{Version: 20260101000001, Status: ledger.StatusApplied}}
	if _, err := run(db, eng, dir, ".up.sql", "dbtools_migration_history", absent, false); err == nil {
		t.Fatal("run() = nil, want refusal for a migration whose object is genuinely missing")
	}
}
