package adopt_test

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/seanpham99/dbtools/internal/adopt"
	"github.com/seanpham99/dbtools/internal/engine/sqliteengine"
	"github.com/seanpham99/dbtools/internal/ledger"
	"github.com/seanpham99/dbtools/internal/migrator"
)

func openTestSQLite(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqliteengine.SQLite{}.Open("sqlite://" + filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() returned error: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestDetectSourceTable_FirstMatchWins(t *testing.T) {
	exists := func(_ ledger.DBTX, name string) (bool, error) {
		return name == "flyway_schema_history", nil
	}
	table, err := adopt.DetectSourceTable(nil, exists, []string{"schema_migrations", "flyway_schema_history"})
	if err != nil {
		t.Fatalf("DetectSourceTable() returned error: %v", err)
	}
	if table != "flyway_schema_history" {
		t.Errorf("table = %q, want flyway_schema_history", table)
	}
}

func TestDetectSourceTable_NoneFound(t *testing.T) {
	exists := func(_ ledger.DBTX, _ string) (bool, error) { return false, nil }
	_, err := adopt.DetectSourceTable(nil, exists, []string{"schema_migrations"})
	if err == nil {
		t.Fatal("DetectSourceTable() with no match: want error, got nil")
	}
}

func TestBuildPlan_ThreeWayDiff(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "1_a.up.sql"), []byte("-- sql"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2_b.up.sql"), []byte("-- sql"), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := migrator.ReadDir(dir, ".up.sql")
	if err != nil {
		t.Fatal(err)
	}

	sourceRows := []adopt.SourceRow{{Version: 1}, {Version: 3}}
	plan := adopt.BuildPlan("schema_migrations", sourceRows, d)

	if len(plan.Matched) != 1 || plan.Matched[0] != 1 {
		t.Errorf("Matched = %v, want [1]", plan.Matched)
	}
	if len(plan.Pending) != 1 || plan.Pending[0] != 2 {
		t.Errorf("Pending = %v, want [2]", plan.Pending)
	}
	if len(plan.Orphan) != 1 || plan.Orphan[0] != 3 {
		t.Errorf("Orphan = %v, want [3]", plan.Orphan)
	}
}

func TestReadSourceRows_SQLite(t *testing.T) {
	db := openTestSQLite(t)

	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TIMESTAMP)`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := db.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (1, ?), (2, ?)`, now, now); err != nil {
		t.Fatal(err)
	}

	rows, err := adopt.ReadSourceRows(db, "schema_migrations", "version", "applied_at")
	if err != nil {
		t.Fatalf("ReadSourceRows() returned error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0].Version != 1 || rows[1].Version != 2 {
		t.Errorf("rows versions = %d, %d, want 1, 2", rows[0].Version, rows[1].Version)
	}
}

func TestReadSourceRows_StringVersions(t *testing.T) {
	db := openTestSQLite(t)

	if _, err := db.Exec(`CREATE TABLE __EFMigrationsHistory (MigrationId TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO __EFMigrationsHistory (MigrationId) VALUES ('20260822000001_Initial'), ('20260822000002_AddUsers')`); err != nil {
		t.Fatal(err)
	}

	rows, err := adopt.ReadSourceRows(db, "__EFMigrationsHistory", "MigrationId", "")
	if err != nil {
		t.Fatalf("ReadSourceRows() returned error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}
	if rows[0].Version != 20260822000001 || rows[1].Version != 20260822000002 {
		t.Errorf("rows versions = %d, %d, want 20260822000001, 20260822000002", rows[0].Version, rows[1].Version)
	}
}

// TestReadSourceRows_RejectsDottedVersions is a regression test for a
// review finding: a Flyway-style dotted version ("V1.1", "V1.2") used to
// silently truncate to its leading digit, colliding two distinct rows onto
// the same uint64 version and corrupting the adopted ledger.
func TestReadSourceRows_RejectsDottedVersions(t *testing.T) {
	db := openTestSQLite(t)

	if _, err := db.Exec(`CREATE TABLE flyway_schema_history (version TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO flyway_schema_history (version) VALUES ('1.1')`); err != nil {
		t.Fatal(err)
	}

	if _, err := adopt.ReadSourceRows(db, "flyway_schema_history", "version", ""); err == nil {
		t.Fatal("ReadSourceRows() with dotted version '1.1': want error, got nil")
	}
}

func TestReadSourceRows_RejectsInvalidIdentifiers(t *testing.T) {
	if _, err := adopt.ReadSourceRows(nil, "bad table; name", "version", ""); err == nil {
		t.Fatal("ReadSourceRows() with invalid table name: want error, got nil")
	}
	if _, err := adopt.ReadSourceRows(nil, "schema_migrations", "bad col; name", ""); err == nil {
		t.Fatal("ReadSourceRows() with invalid version column: want error, got nil")
	}
	if _, err := adopt.ReadSourceRows(nil, "schema_migrations", "version", "bad applied; col"); err == nil {
		t.Fatal("ReadSourceRows() with invalid applied_at column: want error, got nil")
	}
}

func TestBuildPlan_EmptyPlanMarshalsAsEmptyArrays(t *testing.T) {
	plan := adopt.BuildPlan("schema_migrations", nil, nil)
	b, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{`"matched":[]`, `"pending":[]`, `"orphan":[]`} {
		if !strings.Contains(got, want) {
			t.Fatalf("marshaled plan = %s, want it to contain %s", got, want)
		}
	}
}
