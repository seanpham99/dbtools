//go:build integration

package repair

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/seanpham99/dbtools/internal/engine/mssqlengine"
	"github.com/seanpham99/dbtools/internal/ledger"
	"github.com/seanpham99/dbtools/internal/migrator"
	"github.com/seanpham99/dbtools/internal/testdb"
)

func setupTest(t *testing.T) (dir, url string) {
	t.Helper()
	url = os.Getenv("DBTOOLS_TEST_MSSQL_URL")
	if url == "" {
		t.Skip("DBTOOLS_TEST_MSSQL_URL not set, skipping integration test")
	}
	if err := testdb.ResetTracking(url); err != nil {
		t.Fatal(err)
	}
	dir = t.TempDir()
	return dir, url
}

func TestRun_RefusesWithoutForceWhenObjectMissing(t *testing.T) {
	dir, url := setupTest(t)
	if err := os.WriteFile(filepath.Join(dir, "20260101000000_create_widgets.up.sql"),
		[]byte("CREATE TABLE dbtools_test_repair_widgets (id INT PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := mssqlengine.Open(url)
	if err != nil {
		t.Fatalf("dbconn.Open() returned error: %v", err)
	}
	defer db.Close()

	// The table was never actually created (reproducing the real
	// incident) — repair must refuse to mark it applied without --force.
	pairs := []Pair{{Version: 20260101000000, Status: ledger.StatusApplied}}
	if _, err := Run(db, mssqlengine.MSSQL{}, dir, ".up.sql", "dbtools_migration_history", pairs, false); err == nil {
		t.Fatal("expected Run() to refuse marking applied when object is missing, got nil error")
	}

	if _, err := Run(db, mssqlengine.MSSQL{}, dir, ".up.sql", "dbtools_migration_history", pairs, true); err != nil {
		t.Fatalf("Run() with force=true returned error: %v", err)
	}

	entries, err := mssqlengine.List(db, "dbtools_migration_history")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Status != ledger.StatusApplied {
		t.Fatalf("ledger.List() = %+v, want one applied entry after forced repair", entries)
	}
}

func TestRun_RecomputesCursor(t *testing.T) {
	dir, url := setupTest(t)
	for _, f := range []struct{ name, sql string }{
		{"20260101000000_create_a.up.sql", "CREATE TABLE dbtools_test_repair_a (id INT PRIMARY KEY);"},
		{"20260102000000_create_b.up.sql", "CREATE TABLE dbtools_test_repair_b (id INT PRIMARY KEY);"},
	} {
		if err := os.WriteFile(filepath.Join(dir, f.name), []byte(f.sql), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	db, err := mssqlengine.Open(url)
	if err != nil {
		t.Fatalf("dbconn.Open() returned error: %v", err)
	}
	defer db.Close()

	d, err := migrator.ReadDir(dir, ".up.sql")
	if err != nil {
		t.Fatal(err)
	}
	eng := mssqlengine.MSSQL{}
	if _, err := migrator.NewRunner(eng, db, d, "dbtools_migration_history").Up(context.Background()); err != nil {
		t.Fatalf("Up() returned error: %v", err)
	}

	// Mark the later version reverted — cursor should recompute down to
	// the earlier still-applied version.
	pairs := []Pair{{Version: 20260102000000, Status: ledger.StatusReverted}}
	result, err := Run(db, mssqlengine.MSSQL{}, dir, ".up.sql", "dbtools_migration_history", pairs, false)
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}
	if !result.HasCursor || result.NewCursor != 20260101000000 {
		t.Fatalf("Run() result = %+v, want cursor recomputed to 20260101000000", result)
	}

	state, err := eng.Ledger().State(db, "dbtools_migration_history")
	if err != nil {
		t.Fatalf("State() returned error: %v", err)
	}
	if !state.HasVersion || state.Dirty || state.Version != 20260101000000 {
		t.Errorf("State() = %+v, want version 20260101000000, applied, not dirty", state)
	}
}

func TestRun_RevertsWithoutFilePresent(t *testing.T) {
	dir, url := setupTest(t)

	db, err := mssqlengine.Open(url)
	if err != nil {
		t.Fatalf("dbconn.Open() returned error: %v", err)
	}
	defer db.Close()

	// Reproduces a migration file getting renamed away after it was applied
	// — dir has no file for this version at all. Marking it reverted must
	// not require finding a file, since there's nothing to check for a
	// reverted version.
	pairs := []Pair{{Version: 20260101000000, Status: ledger.StatusReverted}}
	if _, err := Run(db, mssqlengine.MSSQL{}, dir, ".up.sql", "dbtools_migration_history", pairs, false); err != nil {
		t.Fatalf("Run() marking reverted with no file present returned error: %v", err)
	}

	entries, err := mssqlengine.List(db, "dbtools_migration_history")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Status != ledger.StatusReverted {
		t.Fatalf("ledger.List() = %+v, want one reverted entry", entries)
	}
}

func TestRun_UnknownVersionRejected(t *testing.T) {
	dir, url := setupTest(t)
	db, err := mssqlengine.Open(url)
	if err != nil {
		t.Fatalf("dbconn.Open() returned error: %v", err)
	}
	defer db.Close()

	pairs := []Pair{{Version: 99999999999999, Status: ledger.StatusApplied}}
	if _, err := Run(db, mssqlengine.MSSQL{}, dir, ".up.sql", "dbtools_migration_history", pairs, true); err == nil {
		t.Fatal("expected error for a version with no matching migration file, got nil")
	}
}
