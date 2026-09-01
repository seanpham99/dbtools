package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPushCmd_RequiresYesFlag(t *testing.T) {
	pushYes = false
	err := pushCmd.RunE(pushCmd, []string{"nonexistent-target"})
	if err == nil {
		t.Fatal("expected error when --yes is not set, got nil")
	}
}

func TestPushSilenceUsageOnRefusal(t *testing.T) {
	origSilenceUsage := pushCmd.SilenceUsage
	pushCmd.SilenceUsage = false
	t.Cleanup(func() { pushCmd.SilenceUsage = origSilenceUsage })

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

	var out strings.Builder
	rootCmd.SetErr(&out)
	rootCmd.SetOut(&out)
	t.Cleanup(func() { rootCmd.SetErr(nil); rootCmd.SetOut(nil) })

	rootCmd.SetArgs([]string{"push", "prod"})
	err = rootCmd.Execute()
	if err == nil {
		t.Fatal("push prod without --yes expected error, got nil")
	}
	if strings.Contains(out.String(), "Usage:") {
		t.Fatalf("push operational refusal printed usage block:\n%s", out.String())
	}
}
