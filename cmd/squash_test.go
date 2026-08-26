package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/apply"
	"github.com/seanpham99/dbtools/internal/config"
	_ "modernc.org/sqlite"
)

func setupSquashCmdTestEnv(t *testing.T, protected bool) (string, string, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "target.db")
	rawURL := fmt.Sprintf("sqlite://%s", dbPath)
	migrationsDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	m1Up := `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);`
	m2Up := `CREATE TABLE orders (id INTEGER PRIMARY KEY, user_id INTEGER);`
	if err := os.WriteFile(filepath.Join(migrationsDir, "20260822000001_users.up.sql"), []byte(m1Up), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(migrationsDir, "20260822000002_orders.up.sql"), []byte(m2Up), 0o644); err != nil {
		t.Fatal(err)
	}

	cfgContent := fmt.Sprintf(`migrations_dir = %q
[targets.testdb]
url_env = "DBTOOLS_TEST_CMD_SQUASH_URL"
engine = "sqlite"
protected = %t
`, migrationsDir, protected)

	configPath := filepath.Join(dir, "dbtools.toml")
	if err := os.WriteFile(configPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("DBTOOLS_TEST_CMD_SQUASH_URL", rawURL)

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}

	return dir, dbPath, cfg
}

func TestSquashCommand_DryRunWritesNothing(t *testing.T) {
	dir, _, _ := setupSquashCmdTestEnv(t, false)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	squashYes = false
	squashDryRun = false
	squashUpto = 0
	squashOut = ""

	if err := runSquash("testdb"); err != nil {
		t.Fatalf("runSquash returned error: %v", err)
	}

	// Assert no baseline written
	migrationsDir := filepath.Join(dir, "migrations")
	if _, err := os.Stat(filepath.Join(migrationsDir, "0000000000000_squashed_baseline.up.sql")); !os.IsNotExist(err) {
		t.Fatal("dry-run wrote baseline file")
	}
	if _, err := os.Stat(filepath.Join(migrationsDir, "_archived")); !os.IsNotExist(err) {
		t.Fatal("dry-run created archive directory")
	}
}

func TestSquashCommand_YesWritesAndRestamps(t *testing.T) {
	dir, _, cfg := setupSquashCmdTestEnv(t, false)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	// Fully apply first so target cursor is at version 20260822000002
	if _, err := apply.Run(cfg, "testdb", ""); err != nil {
		t.Fatalf("initial apply.Run: %v", err)
	}

	squashYes = true
	squashDryRun = false
	squashUpto = 0
	squashOut = ""

	if err := runSquash("testdb"); err != nil {
		t.Fatalf("runSquash with --yes failed: %v", err)
	}

	migrationsDir := filepath.Join(dir, "migrations")
	if _, err := os.Stat(filepath.Join(migrationsDir, "0000000000000_squashed_baseline.up.sql")); err != nil {
		t.Fatalf("baseline file not found: %v", err)
	}
	if _, err := os.Stat(filepath.Join(migrationsDir, "_archived", "20260822000001_users.up.sql")); err != nil {
		t.Fatalf("archived file 1 not found: %v", err)
	}
	if _, err := os.Stat(filepath.Join(migrationsDir, "_archived", "20260822000002_orders.up.sql")); err != nil {
		t.Fatalf("archived file 2 not found: %v", err)
	}

	// Verify a second apply.Run succeeds without error!
	status, err := apply.Run(cfg, "testdb", "")
	if err != nil {
		t.Fatalf("subsequent apply.Run failed: %v", err)
	}
	if status.CurrentVersion != 0 {
		t.Errorf("current version = %d, want 0", status.CurrentVersion)
	}
}

func TestSquashCommand_ProtectedTargetRefuses(t *testing.T) {
	dir, _, _ := setupSquashCmdTestEnv(t, true)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	squashYes = true
	squashDryRun = false
	squashUpto = 0
	squashOut = ""

	err = runSquash("testdb")
	if err == nil || !strings.Contains(err.Error(), "protected") {
		t.Fatalf("runSquash on protected target with --yes = %v, want protected refusal", err)
	}
}
