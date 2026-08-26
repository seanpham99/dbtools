package cmd

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/apply"
	"github.com/seanpham99/dbtools/internal/config"
	_ "modernc.org/sqlite"
)

func setupDiffCmdTestEnv(t *testing.T) (string, string, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "target.db")
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
url_env = "DBTOOLS_TEST_CMD_DIFF_URL"
engine = "sqlite"
protected = false
`, migrationsDir)

	configPath := filepath.Join(dir, "dbtools.toml")
	if err := os.WriteFile(configPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("DBTOOLS_TEST_CMD_DIFF_URL", rawURL)

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}

	return dir, dbPath, cfg
}

func TestDiffCommand_MissingTargetStillPrintsUsage(t *testing.T) {
	origSilenceUsage := diffCmd.SilenceUsage
	diffCmd.SilenceUsage = false
	t.Cleanup(func() { diffCmd.SilenceUsage = origSilenceUsage })

	var out strings.Builder
	rootCmd.SetErr(&out)
	rootCmd.SetOut(&out)
	t.Cleanup(func() { rootCmd.SetErr(nil); rootCmd.SetOut(nil) })

	rootCmd.SetArgs([]string{"diff"})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("diff with no target returned nil error, want an argument-count error")
	}
	if !strings.Contains(out.String(), "Usage:") {
		t.Fatalf("diff with a missing required argument did not print usage: %s", out.String())
	}
}

func TestDiffCommand_CleanReportsExitZero(t *testing.T) {
	dir, _, cfg := setupDiffCmdTestEnv(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if _, err := apply.Run(cfg, "testdb", ""); err != nil {
		t.Fatalf("apply.Run failed: %v", err)
	}

	diffAgainst = ""
	jsonOutput = false
	if err := runDiff("testdb"); err != nil {
		t.Fatalf("runDiff returned error: %v, want nil", err)
	}
}

func TestDiffCommand_AgainstTargetURLRejectedBeforeReplay(t *testing.T) {
	dir, dbPath, _ := setupDiffCmdTestEnv(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	diffAgainst = "sqlite://" + dbPath
	t.Cleanup(func() { diffAgainst = "" })

	err = runDiff("testdb")
	if err == nil || !strings.Contains(err.Error(), "--against must not match") {
		t.Fatalf("runDiff with target URL as --against returned %v, want a matching-URL error", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`).Scan(&count); err != nil {
		t.Fatalf("checking for replay-created tables: %v", err)
	}
	if count != 0 {
		t.Fatalf("found %d table(s) after matching --against rejection; migration replay started", count)
	}
}

func TestDiffCommand_DistinctAgainstURLStillRuns(t *testing.T) {
	dir, _, cfg := setupDiffCmdTestEnv(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if _, err := apply.Run(cfg, "testdb", ""); err != nil {
		t.Fatalf("apply.Run failed: %v", err)
	}

	diffAgainst = "sqlite://" + filepath.Join(dir, "scratch.db")
	t.Cleanup(func() { diffAgainst = "" })
	jsonOutput = false
	if err := runDiff("testdb"); err != nil {
		t.Fatalf("runDiff with distinct --against URL returned error: %v", err)
	}
}

func TestDiffCommand_FindingsReportExitTwo(t *testing.T) {
	dir, dbPath, cfg := setupDiffCmdTestEnv(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if _, err := apply.Run(cfg, "testdb", ""); err != nil {
		t.Fatalf("apply.Run failed: %v", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("ALTER TABLE users ADD COLUMN manual_col TEXT;"); err != nil {
		t.Fatalf("ALTER TABLE failed: %v", err)
	}

	diffAgainst = ""
	jsonOutput = false
	var out string
	out = captureStdout(t, func() { err = runDiff("testdb") })
	if err == nil {
		t.Fatal("runDiff with structural differences returned nil, want ExitCode(2)")
	}
	if !strings.Contains(out, "users.manual_col") {
		t.Fatalf("runDiff output = %q, want named finding formatted as users.manual_col", out)
	}

	var exitErr *ExitCodeError
	if !errors.As(err, &exitErr) {
		t.Fatalf("err is %T (%v), want *ExitCodeError", err, err)
	}
	if exitErr.Code != 2 {
		t.Fatalf("exit code = %d, want 2", exitErr.Code)
	}
}

func TestDiffCommand_TableFindingOmitsTrailingDot(t *testing.T) {
	dir, dbPath, cfg := setupDiffCmdTestEnv(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if _, err := apply.Run(cfg, "testdb", ""); err != nil {
		t.Fatalf("apply.Run failed: %v", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	if _, err := db.Exec("DROP TABLE users;"); err != nil {
		db.Close()
		t.Fatalf("DROP TABLE failed: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("closing target database: %v", err)
	}

	diffAgainst = ""
	jsonOutput = false
	var runErr error
	out := captureStdout(t, func() { runErr = runDiff("testdb") })
	if runErr == nil {
		t.Fatal("runDiff with a missing table returned nil, want ExitCode(2)")
	}
	if strings.Contains(out, "users.") {
		t.Fatalf("runDiff table finding output = %q, want table name without trailing dot", out)
	}
	if !strings.Contains(out, "users") {
		t.Fatalf("runDiff table finding output = %q, want users table name", out)
	}
}

func TestDiffCommand_JSONOutput(t *testing.T) {
	dir, _, cfg := setupDiffCmdTestEnv(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	if _, err := apply.Run(cfg, "testdb", ""); err != nil {
		t.Fatalf("apply.Run failed: %v", err)
	}

	diffAgainst = ""
	jsonOutput = true
	t.Cleanup(func() { jsonOutput = false })

	if err := runDiff("testdb"); err != nil {
		t.Fatalf("runDiff in json mode returned error: %v, want nil", err)
	}
}
