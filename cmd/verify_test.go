package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/engine/sqliteengine"
)

// TestVerifyCommand_MissingTargetStillPrintsUsage guards against RunE's
// exit-2 usage-silencing bleeding into cobra's own argument-count
// validation (Args: cobra.ExactArgs(1)) — that error must still show
// usage, since RunE's SilenceUsage toggle only applies to the documented
// ExitCodeError case and never runs for an argument-count failure.
func TestVerifyCommand_MissingTargetStillPrintsUsage(t *testing.T) {
	// See plan's identical test for why this reset is needed: verifyCmd
	// is a package-level singleton, and an argument-count error never
	// reaches RunE (where SilenceUsage otherwise gets reset per-invocation).
	origSilenceUsage := verifyCmd.SilenceUsage
	verifyCmd.SilenceUsage = false
	t.Cleanup(func() { verifyCmd.SilenceUsage = origSilenceUsage })

	// Cobra writes the error via PrintErrln (stderr) but the usage block
	// via Println (stdout) — capture both into one buffer so the
	// assertion doesn't depend on which stream cobra picks.
	var out strings.Builder
	rootCmd.SetErr(&out)
	rootCmd.SetOut(&out)
	t.Cleanup(func() { rootCmd.SetErr(nil); rootCmd.SetOut(nil) })

	rootCmd.SetArgs([]string{"verify"})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("verify with no target returned nil error, want an argument-count error")
	}
	if !strings.Contains(out.String(), "Usage:") {
		t.Fatalf("verify with a missing required argument did not print usage: %s", out.String())
	}
}

func TestVerifyCommand_NoLedgerDoesNotError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "1_create_widgets.up.sql"),
		[]byte("CREATE TABLE widgets (id INTEGER PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "test.db")
	rawURL := "sqlite://" + dbPath
	t.Setenv("DBTOOLS_TEST_VERIFY_NOLEDGER_URL", rawURL)

	cfgContent := fmt.Sprintf(`migrations_dir = %q
[targets.local]
url_env = "DBTOOLS_TEST_VERIFY_NOLEDGER_URL"
engine = "sqlite"
`, dir)
	configPath := filepath.Join(dir, "dbtools.toml")
	if err := os.WriteFile(configPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	wd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	eng := sqliteengine.SQLite{}
	db, err := eng.Open(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	// widgets is never created and dbtools_migration_history never exists.
	db.Close()

	verifyInitLedger = false
	err = runVerify("local")
	var exitErr *ExitCodeError
	if err != nil && !(errors.As(err, &exitErr) && exitErr.Code == 2) {
		t.Fatalf("runVerify() with no ledger = %v, want nil or exit-2 drift, not a hard failure", err)
	}
}

func TestVerifyCommand_InitLedgerRefusesProtectedTarget(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "1_create_widgets.up.sql"),
		[]byte("CREATE TABLE widgets (id INTEGER PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "test.db")
	rawURL := "sqlite://" + dbPath
	t.Setenv("DBTOOLS_TEST_VERIFY_PROT_URL", rawURL)

	cfgContent := fmt.Sprintf(`migrations_dir = %q
[targets.prod]
url_env = "DBTOOLS_TEST_VERIFY_PROT_URL"
engine = "sqlite"
protected = true
`, dir)
	configPath := filepath.Join(dir, "dbtools.toml")
	if err := os.WriteFile(configPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	wd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	verifyInitLedger = true
	t.Cleanup(func() { verifyInitLedger = false })

	err := runVerify("prod")
	if err == nil || !strings.Contains(err.Error(), "protected") {
		t.Fatalf("runVerify(prod, --init-ledger) = %v, want protected target error", err)
	}
}
