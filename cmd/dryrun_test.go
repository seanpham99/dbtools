package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seanpham99/dbtools/internal/config"
)

func TestUpAndPush_DryRun(t *testing.T) {
	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
		jsonOutput = false
		upTarget = "local"
		upURL = ""
		upDryRun = false
		pushURL = ""
		pushDryRun = false
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll("migrations", 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join("migrations", "20260101000000_init.up.sql"), []byte("CREATE TABLE dry_test (id INT);"), 0o644)

	dbURL := "sqlite://" + filepath.Join(dir, "dry_test.db")
	t.Setenv("DBTOOLS_TEST_DRY_URL", dbURL)

	configContent := `migrations_dir = "migrations"

[targets.local]
url_env = "DBTOOLS_TEST_DRY_URL"

[targets.remote]
url_env = "DBTOOLS_TEST_DRY_URL"
`
	if err := os.WriteFile("dbtools.toml", []byte(configContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. up --dry-run
	upTarget = "local"
	upURL = ""
	upDryRun = true
	jsonOutput = false
	if err := upCmd.RunE(upCmd, nil); err != nil {
		t.Fatalf("up --dry-run failed: %v", err)
	}

	// 2. up --dry-run --json
	jsonOutput = true
	if err := upCmd.RunE(upCmd, nil); err != nil {
		t.Fatalf("up --dry-run --json failed: %v", err)
	}

	// 3. push remote --dry-run
	pushURL = ""
	pushDryRun = true
	jsonOutput = false
	if err := pushCmd.RunE(pushCmd, []string{"remote"}); err != nil {
		t.Fatalf("push --dry-run failed: %v", err)
	}

	// 4. push remote --dry-run --json
	jsonOutput = true
	if err := pushCmd.RunE(pushCmd, []string{"remote"}); err != nil {
		t.Fatalf("push --dry-run --json failed: %v", err)
	}
}

// A dry run must preview the database it was asked about.
//
// Routing this through OpenTarget broke both halves of that: OpenTarget
// re-resolves the configured target, so --url was silently ignored, and it
// calls engine.EnsureDatabase for unprotected targets, so previewing a
// server database that did not exist yet would create it.
//
// The --url half is what this pins. (Opening a SQLite target still creates
// an empty file — that is the driver, not provisioning, and it happens for
// every read-only command including status.)
func TestDryRun_PreviewsTheURLOverride(t *testing.T) {
	dir := t.TempDir()
	migDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(migDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(migDir, "20260101000000_init.up.sql"),
		[]byte("CREATE TABLE t (id INTEGER PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}

	configured := filepath.Join(dir, "configured.db")
	override := filepath.Join(dir, "override.db")
	t.Setenv("DBTOOLS_DRYRUN_CONFIGURED_URL", "sqlite://"+configured)
	cfg := &config.Config{
		MigrationsDir: migDir,
		Targets:       map[string]config.Target{"local": {URLEnv: "DBTOOLS_DRYRUN_CONFIGURED_URL"}},
	}

	if err := runDryRun(cfg, "local", "sqlite://"+override); err != nil {
		t.Fatalf("runDryRun() with --url returned error: %v", err)
	}
	if _, err := os.Stat(override); err != nil {
		t.Errorf("--url database was never opened: %v — the preview used the configured target instead", err)
	}
	if _, err := os.Stat(configured); err == nil {
		t.Error("the configured target was opened despite --url pointing elsewhere")
	}
}
