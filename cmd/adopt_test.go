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
	"github.com/seanpham99/dbtools/internal/migrator"
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

	// Verify migrator cursor is stamped
	m, err := migrator.Open(rawURL, cfg.MigrationsDir)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	curVer, _, hasVer, err := m.Version()
	if err != nil || !hasVer || curVer != 20260822000001 {
		t.Errorf("migrator cursor = (%d, %v), want (20260822000001, true)", curVer, hasVer)
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
