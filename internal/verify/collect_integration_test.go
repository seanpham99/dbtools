//go:build integration

package verify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbtools/dbtools/internal/dbconn"
	"github.com/dbtools/dbtools/internal/engine/mssqlengine"
	"github.com/dbtools/dbtools/internal/ledger"
	"github.com/dbtools/dbtools/internal/testdb"
)

func TestCollect_DetectsStampedButNeverRunTable(t *testing.T) {
	url := os.Getenv("DBTOOLS_TEST_MSSQL_URL")
	if url == "" {
		t.Skip("DBTOOLS_TEST_MSSQL_URL not set, skipping integration test")
	}
	if err := testdb.ResetTracking(url); err != nil {
		t.Fatal(err)
	}

	db, err := dbconn.Open(url)
	if err != nil {
		t.Fatalf("dbconn.Open() returned error: %v", err)
	}
	defer db.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "20260101000000_create_widgets.up.sql"),
		[]byte("CREATE TABLE dbtools_test_verify_widgets (id INT PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ledger.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	// Reproduces the 2026-07-10 incident: the ledger claims this version is
	// applied, but its CREATE TABLE was never actually run.
	if err := ledger.SetStatus(db, 20260101000000, ledger.StatusApplied, "simulated stamp-without-running"); err != nil {
		t.Fatal(err)
	}

	report, err := Collect(db, mssqlengine.MSSQL{}, dir, "test-target")
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	if len(report.Entries) != 1 || report.Entries[0].Status != "DRIFT" {
		t.Fatalf("Collect() = %+v, want one DRIFT entry", report.Entries)
	}

	// Now actually create the table (as the real fix migration did) and
	// confirm verify reports OK.
	if _, err := db.Exec("CREATE TABLE dbtools_test_verify_widgets (id INT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP TABLE dbtools_test_verify_widgets")

	report, err = Collect(db, mssqlengine.MSSQL{}, dir, "test-target")
	if err != nil {
		t.Fatalf("second Collect() returned error: %v", err)
	}
	if len(report.Entries) != 1 || report.Entries[0].Status != "OK" {
		t.Fatalf("second Collect() = %+v, want one OK entry after creating the table", report.Entries)
	}
}

func TestCollect_RevertedButStillExists(t *testing.T) {
	url := os.Getenv("DBTOOLS_TEST_MSSQL_URL")
	if url == "" {
		t.Skip("DBTOOLS_TEST_MSSQL_URL not set, skipping integration test")
	}
	if err := testdb.ResetTracking(url); err != nil {
		t.Fatal(err)
	}

	db, err := dbconn.Open(url)
	if err != nil {
		t.Fatalf("dbconn.Open() returned error: %v", err)
	}
	defer db.Close()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "20260101000000_create_widgets.up.sql"),
		[]byte("CREATE TABLE dbtools_test_verify_reverted (id INT PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE dbtools_test_verify_reverted (id INT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP TABLE dbtools_test_verify_reverted")

	if err := ledger.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := ledger.SetStatus(db, 20260101000000, ledger.StatusReverted, "test"); err != nil {
		t.Fatal(err)
	}

	report, err := Collect(db, mssqlengine.MSSQL{}, dir, "test-target")
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	if len(report.Entries) != 1 || report.Entries[0].Status != "DRIFT" {
		t.Fatalf("Collect() = %+v, want one DRIFT entry (reverted but object still exists)", report.Entries)
	}
}

func TestCollect_RevertedWithFileGoneIsNotDrift(t *testing.T) {
	url := os.Getenv("DBTOOLS_TEST_MSSQL_URL")
	if url == "" {
		t.Skip("DBTOOLS_TEST_MSSQL_URL not set, skipping integration test")
	}
	if err := testdb.ResetTracking(url); err != nil {
		t.Fatal(err)
	}

	db, err := dbconn.Open(url)
	if err != nil {
		t.Fatalf("dbconn.Open() returned error: %v", err)
	}
	defer db.Close()

	// Reproduces a migration file getting renamed/squashed away (e.g. a later
	// PR splits it into new files) after its version was already marked
	// reverted — the ledger row has no file to check against, but it isn't
	// drift: reverted already means "these objects shouldn't exist".
	dir := t.TempDir()

	if err := ledger.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := ledger.SetStatus(db, 20260101000000, ledger.StatusReverted, "superseded by a rename, file no longer exists"); err != nil {
		t.Fatal(err)
	}

	report, err := Collect(db, mssqlengine.MSSQL{}, dir, "test-target")
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	if len(report.Entries) != 1 || report.Entries[0].Status != "OK" {
		t.Fatalf("Collect() = %+v, want one OK entry (reverted version with no file is not drift)", report.Entries)
	}
}

func TestCollect_AppliedWithFileGoneIsStillDrift(t *testing.T) {
	url := os.Getenv("DBTOOLS_TEST_MSSQL_URL")
	if url == "" {
		t.Skip("DBTOOLS_TEST_MSSQL_URL not set, skipping integration test")
	}
	if err := testdb.ResetTracking(url); err != nil {
		t.Fatal(err)
	}

	db, err := dbconn.Open(url)
	if err != nil {
		t.Fatalf("dbconn.Open() returned error: %v", err)
	}
	defer db.Close()

	// A version claimed applied with no matching file is still a real
	// problem — there's no reversion sanctioning the file's absence, so this
	// must stay DRIFT even after the reverted-with-missing-file exemption.
	dir := t.TempDir()

	if err := ledger.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := ledger.SetStatus(db, 20260101000000, ledger.StatusApplied, "file missing, still claimed applied"); err != nil {
		t.Fatal(err)
	}

	report, err := Collect(db, mssqlengine.MSSQL{}, dir, "test-target")
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	if len(report.Entries) != 1 || report.Entries[0].Status != "DRIFT" {
		t.Fatalf("Collect() = %+v, want one DRIFT entry (applied version with no file is still drift)", report.Entries)
	}
}

func TestCollect_CreatedThenDroppedByLaterMigrationIsNotDrift(t *testing.T) {
	url := os.Getenv("DBTOOLS_TEST_MSSQL_URL")
	if url == "" {
		t.Skip("DBTOOLS_TEST_MSSQL_URL not set, skipping integration test")
	}
	if err := testdb.ResetTracking(url); err != nil {
		t.Fatal(err)
	}

	db, err := dbconn.Open(url)
	if err != nil {
		t.Fatalf("dbconn.Open() returned error: %v", err)
	}
	defer db.Close()

	// Reproduces the real 2026-07-10 smoke-test scenario at small scale: one
	// migration creates a table, a later one legitimately drops it, both are
	// marked applied — the creating version must report OK, not DRIFT.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "20260101000000_create_widgets.up.sql"),
		[]byte("CREATE TABLE dbtools_test_verify_dropped (id INT PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "20260102000000_drop_widgets.up.sql"),
		[]byte("IF OBJECT_ID('dbtools_test_verify_dropped', 'U') IS NOT NULL\n    DROP TABLE dbtools_test_verify_dropped;"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := ledger.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := ledger.SetStatus(db, 20260101000000, ledger.StatusApplied, "creates the table"); err != nil {
		t.Fatal(err)
	}
	if err := ledger.SetStatus(db, 20260102000000, ledger.StatusApplied, "drops the table"); err != nil {
		t.Fatal(err)
	}

	report, err := Collect(db, mssqlengine.MSSQL{}, dir, "test-target")
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	if len(report.Entries) != 2 {
		t.Fatalf("Collect() = %+v, want 2 entries", report.Entries)
	}
	for _, e := range report.Entries {
		if e.Status != "OK" {
			t.Errorf("Collect() entry %+v, want status OK (drop-explained absence is not drift)", e)
		}
	}
}

func TestCollect_ReportsAllMissingObjectsInOneMigration(t *testing.T) {
	url := os.Getenv("DBTOOLS_TEST_MSSQL_URL")
	if url == "" {
		t.Skip("DBTOOLS_TEST_MSSQL_URL not set, skipping integration test")
	}
	if err := testdb.ResetTracking(url); err != nil {
		t.Fatal(err)
	}

	db, err := dbconn.Open(url)
	if err != nil {
		t.Fatalf("dbconn.Open() returned error: %v", err)
	}
	defer db.Close()

	// A single migration claiming to create two objects, neither of which
	// actually exists, must report both in Detail — not just the last one
	// checked (a real prod incident hit exactly this: a migration created a
	// table and a procedure, only the table was missing, but the ledger
	// still needs both surfaced when more than one object drifts at once).
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "20260101000000_create_two_things.up.sql"),
		[]byte("CREATE TABLE dbtools_test_verify_multi_table (id INT PRIMARY KEY);\nGO\nCREATE PROCEDURE dbtools_test_verify_multi_proc AS BEGIN SELECT 1; END;"),
		0o644); err != nil {
		t.Fatal(err)
	}

	if err := ledger.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	if err := ledger.SetStatus(db, 20260101000000, ledger.StatusApplied, "neither object was actually created"); err != nil {
		t.Fatal(err)
	}

	report, err := Collect(db, mssqlengine.MSSQL{}, dir, "test-target")
	if err != nil {
		t.Fatalf("Collect() returned error: %v", err)
	}
	if len(report.Entries) != 1 || report.Entries[0].Status != "DRIFT" {
		t.Fatalf("Collect() = %+v, want one DRIFT entry", report.Entries)
	}
	detail := report.Entries[0].Detail
	if !strings.Contains(detail, "dbtools_test_verify_multi_table") {
		t.Errorf("Detail = %q, want it to mention the missing table", detail)
	}
	if !strings.Contains(detail, "dbtools_test_verify_multi_proc") {
		t.Errorf("Detail = %q, want it to also mention the missing procedure", detail)
	}
}
