package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/engine/sqliteengine"
	"github.com/seanpham99/dbtools/internal/ledger"
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

	err = runVerify("local")
	var exitErr *ExitCodeError
	if err != nil && !(errors.As(err, &exitErr) && exitErr.Code == 2) {
		t.Fatalf("runVerify() with no ledger = %v, want nil or exit-2 drift, not a hard failure", err)
	}
}

func TestVerifyCommand_DirtyLedgerExitsOne(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "1_create_widgets.up.sql"),
		[]byte("CREATE TABLE widgets (id INTEGER PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "test.db")
	rawURL := "sqlite://" + dbPath
	t.Setenv("DBTOOLS_TEST_VERIFY_DIRTY_URL", rawURL)

	cfgContent := fmt.Sprintf(`migrations_dir = %q
[targets.local]
url_env = "DBTOOLS_TEST_VERIFY_DIRTY_URL"
engine = "sqlite"
`, dir)
	if err := os.WriteFile(filepath.Join(dir, "dbtools.toml"), []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	eng := sqliteengine.SQLite{}
	db, err := eng.Open(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	// Leave a migration mid-apply in the ledger. The separate dirty cursor
	// is gone; an "applying" row is what records the same situation, and it
	// names the migration that died.
	if err := eng.Ledger().EnsureSchema(db, "dbtools_migration_history"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := eng.Ledger().SetStatus(db, 1, ledger.StatusApplying, "died mid-apply", "dbtools_migration_history"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(t.TempDir(), "dbtools")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = repoRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building dbtools: %v\n%s", err, output)
	}

	verify := exec.Command(binary, "verify", "local")
	verify.Dir = dir
	verify.Env = os.Environ()
	output, err := verify.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("dbtools verify error = %v, want process exit error; output:\n%s", err, output)
	}
	if exitErr.ExitCode() != 1 {
		t.Fatalf("dbtools verify exit code = %d, want 1; output:\n%s", exitErr.ExitCode(), output)
	}
	if !strings.Contains(string(output), "migration 1 started and never finished") {
		t.Fatalf("dbtools verify output = %q, want the mid-apply diagnostic", output)
	}
}

// verify is read-only now: --init-ledger was the only path that wrote, and
// it is gone (an empty ledger it created made verify.Collect report a clean
// bill of health for a schema it had never checked). Importing history is
// `dbtools adopt`, which is where the protected-target guard lives.
func TestVerifyCommand_RefusesToVerifyAnEmptyLedger(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "1_create_widgets.up.sql"),
		[]byte("CREATE TABLE widgets (id INTEGER PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(dir, "test.db")
	rawURL := "sqlite://" + dbPath
	t.Setenv("DBTOOLS_TEST_VERIFY_EMPTY_URL", rawURL)

	cfgContent := fmt.Sprintf(`migrations_dir = %q
[targets.local]
url_env = "DBTOOLS_TEST_VERIFY_EMPTY_URL"
engine = "sqlite"
`, dir)
	if err := os.WriteFile(filepath.Join(dir, "dbtools.toml"), []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	eng := sqliteengine.SQLite{}
	db, err := eng.Open(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Ledger().EnsureSchema(db, "dbtools_migration_history"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	wd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	err = runVerify("local")
	if err == nil || !strings.Contains(err.Error(), "dbtools adopt") {
		t.Fatalf("runVerify() over an empty ledger = %v, want a refusal pointing at `dbtools adopt`", err)
	}
}
