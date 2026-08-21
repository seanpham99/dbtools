package cmd

import (
	"os"
	"path/filepath"
	"testing"
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
