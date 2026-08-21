package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUniversalJSON(t *testing.T) {
	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
		jsonOutput = false
		upTarget = "local"
		upDryRun = false
		downPreview = false
		downYes = false
		rollbackYes = false
		repairYes = false
		repairForce = false
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll("migrations", 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join("migrations", "20260101000000_init.up.sql"), []byte("CREATE TABLE jtest (id INT);"), 0o644)
	os.WriteFile(filepath.Join("migrations", "20260101000000_init.down.sql"), []byte("DROP TABLE jtest;"), 0o644)

	dbURL := "sqlite://" + filepath.Join(dir, "jtest.db")
	t.Setenv("DBTOOLS_JSON_TEST_URL", dbURL)

	configContent := `migrations_dir = "migrations"

[targets.local]
url_env = "DBTOOLS_JSON_TEST_URL"
`
	if err := os.WriteFile("dbtools.toml", []byte(configContent), 0o644); err != nil {
		t.Fatal(err)
	}

	jsonOutput = true

	// 1. up --json
	upTarget = "local"
	upURL = ""
	upDryRun = false
	if err := upCmd.RunE(upCmd, nil); err != nil {
		t.Fatalf("up --json failed: %v", err)
	}

	// 2. status --json
	statusTarget = ""
	statusURL = ""
	if err := statusCmd.RunE(statusCmd, nil); err != nil {
		t.Fatalf("status --json failed: %v", err)
	}

	// 3. verify --json
	if err := verifyCmd.RunE(verifyCmd, []string{"local"}); err != nil {
		t.Fatalf("verify --json failed: %v", err)
	}

	// 4. down --preview --json
	downPreview = true
	downYes = false
	downURL = ""
	if err := downCmd.RunE(downCmd, []string{"local", "1"}); err != nil {
		t.Fatalf("down --preview --json failed: %v", err)
	}

	// 5. down --json
	downPreview = false
	downYes = true
	if err := downCmd.RunE(downCmd, []string{"local", "1"}); err != nil {
		t.Fatalf("down --json failed: %v", err)
	}

	// 6. repair --json (with repairForce=true because table was dropped by down)
	repairYes = true
	repairForce = true
	if err := repairCmd.RunE(repairCmd, []string{"local", "20260101000000:applied"}); err != nil {
		t.Fatalf("repair --json failed: %v", err)
	}

	// 7. rollback --json
	rollbackYes = true
	rollbackURL = ""
	if err := rollbackCmd.RunE(rollbackCmd, []string{"local", "1"}); err != nil {
		t.Fatalf("rollback --json failed: %v", err)
	}
}
