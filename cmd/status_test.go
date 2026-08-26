package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine/sqliteengine"
	"github.com/seanpham99/dbtools/internal/migrator"
	"github.com/seanpham99/dbtools/internal/statusinfo"
)

func TestRenderStatusTable(t *testing.T) {
	results := []statusinfo.TargetResult{
		{
			Target: "local",
			Status: &statusinfo.Status{
				Target:         "local",
				CurrentVersion: 20260101000000,
				HasVersion:     true,
				Dirty:          false,
				Pending:        nil,
			},
		},
		{
			Target:       "prod",
			Unconfigured: true,
		},
		{
			Target: "staging",
			Status: &statusinfo.Status{
				Target:         "staging",
				CurrentVersion: 20260101000000,
				HasVersion:     true,
				Dirty:          true,
				Pending:        []string{"20260102000000_add.up.sql"},
			},
		},
	}

	out := renderStatusTable(results)

	if !strings.Contains(out, "local       up to date") {
		t.Errorf("rendered output missing local status: %s", out)
	}
	if !strings.Contains(out, "prod        [unconfigured]") {
		t.Errorf("rendered output missing unconfigured prod status: %s", out)
	}
	if !strings.Contains(out, "staging     1 pending [DIRTY]") {
		t.Errorf("rendered output missing dirty staging status: %s", out)
	}
}

// TestRunStatus_ExitsNonZeroOnConnectionFailure guards the exit-code
// contract: a target status can't be collected (here: an unregistered
// engine scheme, which fails before any network call) must exit 1, not
// print "error:" on stdout while returning success.
func TestRunStatus_ExitsNonZeroOnConnectionFailure(t *testing.T) {
	origLoadConfig := loadConfig
	t.Cleanup(func() { loadConfig = origLoadConfig })

	loadConfig = func(string) (*config.Config, error) {
		return &config.Config{
			MigrationsDir: "migrations",
			Targets:       map[string]config.Target{"local": {URLEnv: "DBTOOLS_STATUS_TEST_URL"}},
		}, nil
	}
	t.Setenv("DBTOOLS_STATUS_TEST_URL", "nosuchengine://host/db")

	err := runStatus()
	if err == nil {
		t.Fatal("runStatus() with an unreachable target returned nil error, want a non-zero exit")
	}
	var exitErr *ExitCodeError
	if !errors.As(err, &exitErr) {
		t.Fatalf("runStatus() error = %v (%T), want *ExitCodeError", err, err)
	}
	if exitErr.Code != 1 {
		t.Fatalf("runStatus() exit code = %d, want 1", exitErr.Code)
	}
}

func TestRunStatus_NoLedgerReportsNoLedgerTrue(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join("migrations", "20260817000001_users.up.sql"), []byte(`CREATE TABLE users (id INTEGER PRIMARY KEY);`), 0o644); err != nil {
		t.Fatal(err)
	}

	dbURL := "sqlite://" + filepath.Join(dir, "local.db")
	t.Setenv("DBTOOLS_STATUS_TEST_NOLEDGER_URL", dbURL)
	cfg := &config.Config{
		MigrationsDir: "migrations",
		Targets:       map[string]config.Target{"local": {URLEnv: "DBTOOLS_STATUS_TEST_NOLEDGER_URL"}},
	}
	origLoadConfig := loadConfig
	t.Cleanup(func() { loadConfig = origLoadConfig })
	loadConfig = func(string) (*config.Config, error) { return cfg, nil }

	// Create users table and stamp the cursor (so status has HasVersion: true),
	// but never create dbtools_migration_history.
	eng := sqliteengine.SQLite{}
	db, err := eng.Open(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY);`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	db.Close()

	m, err := migrator.Open(dbURL, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Stamp(20260817000001); err != nil {
		m.Close()
		t.Fatal(err)
	}
	m.Close()

	// JSON mode check: NoLedger is true.
	origJSONOutput := jsonOutput
	jsonOutput = true
	t.Cleanup(func() { jsonOutput = origJSONOutput })

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err = runStatus("local")
	w.Close()
	os.Stdout = oldStdout
	if err != nil {
		t.Fatalf("runStatus() returned error: %v", err)
	}

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	var entries []statusJSONEntry
	if err := json.Unmarshal(buf.Bytes(), &entries); err != nil {
		t.Fatalf("json.Unmarshal(%s) error: %v", buf.String(), err)
	}
	if len(entries) != 1 {
		t.Fatalf("len(entries) = %d, want 1", len(entries))
	}
	if !entries[0].NoLedger {
		t.Errorf("statusJSONEntry.NoLedger = false, want true")
	}

	// Text mode check: prints note about no dbtools ledger.
	jsonOutput = false
	rText, wText, _ := os.Pipe()
	os.Stdout = wText

	err = runStatus("local")
	wText.Close()
	os.Stdout = oldStdout
	if err != nil {
		t.Fatalf("runStatus() returned error: %v", err)
	}

	var bufText bytes.Buffer
	_, _ = io.Copy(&bufText, rText)
	if !strings.Contains(bufText.String(), "no dbtools ledger — run `dbtools adopt` to enable drift tracking") {
		t.Errorf("text output = %q, want it to mention no dbtools ledger", bufText.String())
	}
}

