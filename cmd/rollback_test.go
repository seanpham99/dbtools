package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRollbackCmd_ArgumentValidation(t *testing.T) {
	err := rollbackCmd.RunE(rollbackCmd, []string{"local", "invalid-number"})
	if err == nil {
		t.Fatal("expected error for invalid step number, got nil")
	}
}

func TestRollbackCmd_ProtectedTargetRefusesWithoutYes(t *testing.T) {
	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll("migrations", 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join("migrations", "20260101000000_init.up.sql"), []byte("CREATE TABLE test (id INT);"), 0o644)

	dbURL := "sqlite://" + filepath.Join(dir, "prod.db")
	t.Setenv("DBTOOLS_PROD_URL", dbURL)

	configContent := `migrations_dir = "migrations"

[targets.prod]
url_env = "DBTOOLS_PROD_URL"
protected = true
`
	if err := os.WriteFile("dbtools.toml", []byte(configContent), 0o644); err != nil {
		t.Fatal(err)
	}

	rollbackYes = false
	rollbackURL = ""
	err = rollbackCmd.RunE(rollbackCmd, []string{"prod", "1"})
	if err == nil {
		t.Fatal("expected error when rollback on protected target without --yes, got nil")
	}
}
