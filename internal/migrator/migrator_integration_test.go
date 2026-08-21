//go:build integration

package migrator

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/microsoft/go-mssqldb"

	"github.com/seanpham99/dbtools/internal/testdb"
)

func testDatabaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("DBTOOLS_TEST_MSSQL_URL")
	if url == "" {
		t.Skip("DBTOOLS_TEST_MSSQL_URL not set, skipping integration test")
	}
	return url
}

func writeMigration(t *testing.T, dir, filename, sql string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, filename), []byte(sql), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOpenUpVersion(t *testing.T) {
	url := testDatabaseURL(t)
	if err := testdb.ResetTracking(url); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeMigration(t, dir, "20260101000000_create_widgets.up.sql",
		"CREATE TABLE dbtools_test_widgets (id INT PRIMARY KEY);")

	m, err := Open(url, dir)
	if err != nil {
		t.Fatalf("Open() returned error: %v", err)
	}
	defer m.Close()

	_, _, hasVersion, err := m.Version()
	if err != nil {
		t.Fatalf("Version() before Up() returned error: %v", err)
	}
	if hasVersion {
		t.Fatal("expected hasVersion=false before any migration has run")
	}

	applied, err := m.Up()
	if err != nil {
		t.Fatalf("Up() returned error: %v", err)
	}
	if !applied {
		t.Fatal("expected applied=true on first Up()")
	}

	version, dirty, hasVersion, err := m.Version()
	if err != nil {
		t.Fatalf("Version() after Up() returned error: %v", err)
	}
	if !hasVersion {
		t.Fatal("expected hasVersion=true after Up()")
	}
	if dirty {
		t.Fatal("expected dirty=false after clean Up()")
	}
	if version != 20260101000000 {
		t.Errorf("Version() = %d, want %d", version, 20260101000000)
	}

	// Running Up() again should be a no-op, not an error.
	applied, err = m.Up()
	if err != nil {
		t.Fatalf("second Up() returned error: %v", err)
	}
	if applied {
		t.Fatal("expected applied=false on second Up() (already up to date)")
	}
}

// TestGoBatchSplitting reproduces the real failure this repo's production
// schema hit: two stored procedures, GO-separated, each declaring a local
// variable with the same name. Sending the whole file as one batch (GO
// simply deleted) fails with "variable already declared"; sending "GO" as
// literal text to the driver fails too, since GO is an sqlcmd/SSMS
// convention, not real T-SQL. Only correctly splitting on GO and executing
// each batch separately makes this succeed.
func TestGoBatchSplitting(t *testing.T) {
	url := testDatabaseURL(t)
	if err := testdb.ResetTracking(url); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeMigration(t, dir, "20260101000001_two_procs_same_local_var.up.sql", `
CREATE PROCEDURE dbtools_test_proc_a
AS
BEGIN
	DECLARE @payload INT = 1;
	SELECT @payload;
END;
GO
CREATE PROCEDURE dbtools_test_proc_b
AS
BEGIN
	DECLARE @payload INT = 2;
	SELECT @payload;
END;
GO
`)

	m, err := Open(url, dir)
	if err != nil {
		t.Fatalf("Open() returned error: %v", err)
	}
	defer m.Close()

	if _, err := m.Up(); err != nil {
		t.Fatalf("Up() returned error (GO batch splitting failed): %v", err)
	}

	version, _, hasVersion, err := m.Version()
	if err != nil {
		t.Fatalf("Version() returned error: %v", err)
	}
	if !hasVersion || version != 20260101000001 {
		t.Fatalf("Version() = (version=%d, hasVersion=%v), want (20260101000001, true)", version, hasVersion)
	}

	// Both procedures must actually exist — proves each GO-separated batch
	// was executed, not silently skipped or merged away.
	for _, proc := range []string{"dbtools_test_proc_a", "dbtools_test_proc_b"} {
		db, err := sql.Open("sqlserver", strings.Replace(url, "mssql://", "sqlserver://", 1))
		if err != nil {
			t.Fatal(err)
		}
		var exists int
		err = db.QueryRow("SELECT COUNT(*) FROM sys.procedures WHERE name = @p1", proc).Scan(&exists)
		db.Close()
		if err != nil {
			t.Fatalf("checking procedure %s: %v", proc, err)
		}
		if exists != 1 {
			t.Errorf("procedure %s: expected to exist, found %d", proc, exists)
		}
	}
}

func TestStamp(t *testing.T) {
	url := testDatabaseURL(t)
	if err := testdb.ResetTracking(url); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeMigration(t, dir, "20260101000000_create_widgets.up.sql",
		"CREATE TABLE dbtools_test_stamp (id INT PRIMARY KEY);")

	m, err := Open(url, dir)
	if err != nil {
		t.Fatalf("Open() returned error: %v", err)
	}
	defer m.Close()

	// Stamp marks the version as applied WITHOUT running the migration's SQL.
	if err := m.Stamp(20260101000000); err != nil {
		t.Fatalf("Stamp() returned error: %v", err)
	}

	version, dirty, hasVersion, err := m.Version()
	if err != nil {
		t.Fatalf("Version() after Stamp() returned error: %v", err)
	}
	if !hasVersion || dirty || version != 20260101000000 {
		t.Errorf("after Stamp(): version=%d dirty=%v hasVersion=%v, want version=20260101000000 dirty=false hasVersion=true",
			version, dirty, hasVersion)
	}

	// The table from the migration's SQL must NOT exist — Stamp never ran it.
	applied, err := m.Up()
	if err != nil {
		t.Fatalf("Up() after Stamp() returned error: %v", err)
	}
	if applied {
		t.Fatal("expected Up() to report no change after Stamp() already recorded this version")
	}
}

