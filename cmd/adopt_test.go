package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/seanpham99/dbtools/internal/adopt"
	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine/sqliteengine"
	"github.com/seanpham99/dbtools/internal/ledger"
)

func setupAdoptTestEnv(t *testing.T) (string, string, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "adopt_test.db")
	rawURL := fmt.Sprintf("sqlite://%s", dbPath)
	migrationsDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	m1Up := `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);`
	m1Down := `DROP TABLE users;`
	if err := os.WriteFile(filepath.Join(migrationsDir, "20260822000001_users.up.sql"), []byte(m1Up), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(migrationsDir, "20260822000001_users.down.sql"), []byte(m1Down), 0o644); err != nil {
		t.Fatal(err)
	}

	cfgContent := fmt.Sprintf(`migrations_dir = %q
[targets.testdb]
url_env = "DBTOOLS_TEST_ADOPT_URL"
engine = "sqlite"
protected = false
`, migrationsDir)

	configPath := filepath.Join(dir, "dbtools.toml")
	if err := os.WriteFile(configPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("DBTOOLS_TEST_ADOPT_URL", rawURL)

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}

	return dir, rawURL, cfg
}

func TestAdoptCommand_RefusesWithoutYes(t *testing.T) {
	dir, rawURL, _ := setupAdoptTestEnv(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Create legacy schema_migrations table
	eng := sqliteengine.SQLite{}
	db, err := eng.Open(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, dirty BOOLEAN)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations (version, dirty) VALUES (20260822000001, 0)`); err != nil {
		t.Fatal(err)
	}

	adoptYes = false
	adoptForce = false
	adoptFromTable = ""
	adoptVersionColumn = ""
	adoptAppliedAtColumn = ""

	err = runAdopt("testdb")
	if err != nil {
		t.Fatalf("runAdopt() returned unexpected error: %v", err)
	}

	// Verify nothing was written to dbtools_migration_history
	entries, err := eng.Ledger().List(db, "dbtools_migration_history")
	if err == nil && len(entries) > 0 {
		t.Fatalf("ledger entries written without --yes: %+v", entries)
	}
}

func TestAdoptCommand_RefusesOrphansWithoutForce(t *testing.T) {
	dir, rawURL, _ := setupAdoptTestEnv(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	eng := sqliteengine.SQLite{}
	db, err := eng.Open(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, dirty BOOLEAN)`); err != nil {
		t.Fatal(err)
	}
	// Insert orphan version (no file for 20260822000099)
	if _, err := db.Exec(`INSERT INTO schema_migrations (version, dirty) VALUES (20260822000099, 0)`); err != nil {
		t.Fatal(err)
	}

	adoptYes = true
	adoptForce = false
	adoptFromTable = ""
	adoptVersionColumn = ""
	adoptAppliedAtColumn = ""

	err = runAdopt("testdb")
	var exitErr *ExitCodeError
	if !errors.As(err, &exitErr) || exitErr.Code != 1 {
		t.Fatalf("runAdopt() err = %v, want ExitCodeError with Code=1", err)
	}
}

func TestAdoptCommand_WritesMatchedWithYes(t *testing.T) {
	dir, rawURL, cfg := setupAdoptTestEnv(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	eng := sqliteengine.SQLite{}
	db, err := eng.Open(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, dirty BOOLEAN)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations (version, dirty) VALUES (20260822000001, 0)`); err != nil {
		t.Fatal(err)
	}

	adoptYes = true
	adoptForce = false
	adoptFromTable = ""
	adoptVersionColumn = ""
	adoptAppliedAtColumn = ""

	err = runAdopt("testdb")
	if err != nil {
		t.Fatalf("runAdopt() returned unexpected error: %v", err)
	}

	entries, err := eng.Ledger().List(db, "dbtools_migration_history")
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	e := entries[0]
	if e.Version != 20260822000001 || e.Status != ledger.StatusApplied || e.HashSource != "adopted" {
		t.Errorf("entry = %+v, want version 20260822000001, applied, hash_source=adopted", e)
	}

	// The version is derived from the rows adopt just imported — there is
	// no separate cursor to stamp, which is what made adopt non-atomic
	// (and, on a schema_migrations-named ledger, what made it fail) in #79.
	state, err := eng.Ledger().State(db, cfg.LedgerTableName())
	if err != nil {
		t.Fatalf("reading state: %v", err)
	}
	if !state.HasVersion || state.Version != 20260822000001 {
		t.Errorf("state = %+v, want version 20260822000001", state)
	}
	if state.Dirty {
		t.Errorf("state.Dirty = true after adopt, want false")
	}
}

// TestAdoptCommand_DoesNotBackfillVersionsOutsideSourceTable is a
// regression test for a review finding: adopt used to call
// eng.Ledger().Sync before writing, which also backfills a row (tagged
// with a normal, non-"adopted" hash source) for every version the
// golang-migrate cursor already considers applied — even one the
// incumbent's source table never recorded. That silently produced a
// "verified" ledger row whose hash was never actually checked, which is
// exactly what hash_source="adopted" exists to prevent. adopt must use
// EnsureSchema (create the table only) instead of Sync (create + backfill).
func TestAdoptCommand_DoesNotBackfillVersionsOutsideSourceTable(t *testing.T) {
	dir, rawURL, _ := setupAdoptTestEnv(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// A second migration file dbtools' own cursor will be stamped to,
	// simulating a version applied by dbtools after the incumbent tool's
	// ledger was last updated. The source table below never mentions it.
	m2Up := `CREATE TABLE widgets (id INTEGER PRIMARY KEY);`
	if err := os.WriteFile(filepath.Join(dir, "migrations", "20260822000002_widgets.up.sql"), []byte(m2Up), 0o644); err != nil {
		t.Fatal(err)
	}

	eng := sqliteengine.SQLite{}
	db, err := eng.Open(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// A distinct name from golang-migrate's own "schema_migrations" cursor
	// table (which m.Stamp below writes to) so stamping the migrate
	// cursor doesn't also mutate the simulated incumbent's ledger.
	if _, err := db.Exec(`CREATE TABLE legacy_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO legacy_migrations (version) VALUES (20260822000001)`); err != nil {
		t.Fatal(err)
	}

	// Version 2 exists on disk but the source table never recorded it, so
	// adopt must not import it. (This used to also guard against Sync
	// backfilling it from a separate cursor; with one table there is no
	// second place for a version to come from.)

	adoptYes = true
	adoptForce = false
	adoptFromTable = "legacy_migrations"
	adoptVersionColumn = "version"
	adoptAppliedAtColumn = ""

	if err := runAdopt("testdb"); err != nil {
		t.Fatalf("runAdopt() returned unexpected error: %v", err)
	}

	entries, err := eng.Ledger().List(db, "dbtools_migration_history")
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1 (version 2 must not be backfilled): %+v", len(entries), entries)
	}
	if entries[0].Version != 20260822000001 || entries[0].HashSource != ledger.HashSourceAdopted {
		t.Errorf("entries[0] = %+v, want version 20260822000001 with hash_source=adopted", entries[0])
	}
}

// TestAdoptCommand_RefusesPendingBelowHighestMatched is a regression test
// for a review finding: stamping the migrate cursor to the highest matched
// version while a lower-numbered version is still only "pending" (file on
// disk, no source row) would make that pending version permanently
// unreachable — PendingAfter only returns versions above the cursor.
func TestAdoptCommand_RefusesPendingBelowHighestMatched(t *testing.T) {
	dir, rawURL, _ := setupAdoptTestEnv(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// A second, later migration file with no source-table row: this
	// stays "pending" and must block stamping the cursor past it.
	m2Up := `CREATE TABLE widgets (id INTEGER PRIMARY KEY);`
	if err := os.WriteFile(filepath.Join(dir, "migrations", "20260822000002_widgets.up.sql"), []byte(m2Up), 0o644); err != nil {
		t.Fatal(err)
	}
	// And a third file the source table DOES record, so plan.Matched's
	// highest version (3) is above the pending version (2).
	m3Up := `CREATE TABLE gadgets (id INTEGER PRIMARY KEY);`
	if err := os.WriteFile(filepath.Join(dir, "migrations", "20260822000003_gadgets.up.sql"), []byte(m3Up), 0o644); err != nil {
		t.Fatal(err)
	}

	eng := sqliteengine.SQLite{}
	db, err := eng.Open(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, dirty BOOLEAN)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO schema_migrations (version, dirty) VALUES (20260822000001, 0), (20260822000003, 0)`); err != nil {
		t.Fatal(err)
	}

	adoptYes = true
	adoptForce = false
	adoptFromTable = ""
	adoptVersionColumn = ""
	adoptAppliedAtColumn = ""

	err = runAdopt("testdb")
	if err == nil {
		t.Fatal("runAdopt() with a pending version below the highest matched: want error, got nil")
	}

	// The check runs before EnsureSchema, so the ledger table was never
	// even created — the strongest possible evidence nothing was written.
	entries, listErr := eng.Ledger().List(db, "dbtools_migration_history")
	if listErr == nil && len(entries) != 0 {
		t.Fatalf("entries = %+v, want no writes when the pending-below-matched check fails", entries)
	}
}

func TestAdoptCommand_FromTableOverride(t *testing.T) {
	dir, rawURL, _ := setupAdoptTestEnv(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	eng := sqliteengine.SQLite{}
	db, err := eng.Open(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE custom_hist (v_id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO custom_hist (v_id) VALUES (20260822000001)`); err != nil {
		t.Fatal(err)
	}

	adoptYes = true
	adoptForce = false
	adoptFromTable = "custom_hist"
	adoptVersionColumn = "v_id"
	adoptAppliedAtColumn = ""

	err = runAdopt("testdb")
	if err != nil {
		t.Fatalf("runAdopt() returned unexpected error: %v", err)
	}

	entries, err := eng.Ledger().List(db, "dbtools_migration_history")
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if len(entries) != 1 || entries[0].Version != 20260822000001 {
		t.Fatalf("entries = %+v, want 1 adopted entry", entries)
	}
}

func TestAdoptCommand_JSONOutput(t *testing.T) {
	plan := adopt.Plan{
		SourceTable: "schema_migrations",
		Matched:     []uint64{1, 2},
		Pending:     []uint64{3},
		Orphan:      []uint64{4},
	}
	jsonOutput = true
	t.Cleanup(func() { jsonOutput = false })

	b, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var parsed adopt.Plan
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.SourceTable != "schema_migrations" || len(parsed.Matched) != 2 {
		t.Errorf("parsed plan mismatch: %+v", parsed)
	}
}

func TestAdoptAllowOrphansBefore(t *testing.T) {
	orphans := []uint64{19990101000000, 19990202000000}
	baseline := uint64(20260101000000)

	if !adopt.OrphansBelow(orphans, baseline) {
		t.Fatalf("OrphansBelow(%v, %d) = false, want true", orphans, baseline)
	}

	orphansWithEqual := []uint64{19990101000000, 20260101000000}
	if adopt.OrphansBelow(orphansWithEqual, baseline) {
		t.Fatalf("OrphansBelow(%v, %d) = true, want false when an orphan == baseline", orphansWithEqual, baseline)
	}

	orphansAbove := []uint64{19990101000000, 20270101000000}
	if adopt.OrphansBelow(orphansAbove, baseline) {
		t.Fatalf("OrphansBelow(%v, %d) = true, want false when an orphan > baseline", orphansAbove, baseline)
	}
}
