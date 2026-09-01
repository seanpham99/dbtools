package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/apply"
	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine/sqliteengine"
)

// TestPlanJSONPreview exercises `plan` against a real temp-file SQLite DB:
// after applying v1 only, plan reports v1 as current, v2 pending, no
// drift. Then editing the applied migration file is picked up as
// content-hash drift — the "show me what would happen / what's wrong
// before I act" contract.
func TestPlanJSONPreview(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join("migrations", "20260817000001_users.up.sql"), []byte(`CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL);`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("migrations", "20260817000002_orders.up.sql"), []byte(`CREATE TABLE orders (id INTEGER PRIMARY KEY);`), 0o644); err != nil {
		t.Fatal(err)
	}

	dbURL := "sqlite://" + filepath.Join(dir, "local.db")
	t.Setenv("DBTOOLS_LOCAL_URL", dbURL)
	cfg := &config.Config{
		MigrationsDir: "migrations",
		Targets:       map[string]config.Target{"local": {URLEnv: "DBTOOLS_LOCAL_URL"}},
	}
	loadConfig = func(string) (*config.Config, error) { return cfg, nil }
	t.Cleanup(func() { loadConfig = config.Load })

	// apply v1 only, so v2 stays pending
	if _, err := apply.Run(cfg, "local", ""); err != nil {
		t.Fatalf("apply.Run() returned error: %v", err)
	}
	// apply.Run applies ALL pending; to leave v2 pending, remove it from
	// the dir for the apply, then restore. Simpler: apply the full set,
	// then add a third migration so there's something pending.
	if err := os.WriteFile(filepath.Join("migrations", "20260817000003_audit.up.sql"), []byte(`CREATE TABLE audit_log (id INTEGER PRIMARY KEY);`), 0o644); err != nil {
		t.Fatal(err)
	}

	planTarget = "local"
	defer func() { planTarget = "" }()

	entries := buildPlanEntries(cfg)
	if len(entries) != 1 {
		t.Fatalf("plan entries = %d, want 1", len(entries))
	}
	e := entries[0]
	if e.Target != "local" {
		t.Errorf("plan target = %q, want local", e.Target)
	}
	if e.CurrentVersion != 20260817000002 {
		t.Errorf("plan current_version = %d, want 20260817000002", e.CurrentVersion)
	}
	if e.Dirty {
		t.Errorf("plan dirty = true, want false after clean apply")
	}
	if len(e.Pending) != 1 || e.Pending[0] != "20260817000003_audit.up.sql" {
		t.Errorf("plan pending = %v, want [20260817000003_audit.up.sql]", e.Pending)
	}
	if len(e.Drift) != 0 {
		t.Errorf("plan drift = %v, want none", e.Drift)
	}

	// edit the applied migration -> plan must surface content-hash drift
	if err := os.WriteFile(filepath.Join("migrations", "20260817000001_users.up.sql"), []byte(`CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL, extra TEXT);`), 0o644); err != nil {
		t.Fatal(err)
	}
	entries = buildPlanEntries(cfg)
	if len(entries) != 1 {
		t.Fatalf("plan entries after edit = %d, want 1", len(entries))
	}
	if len(entries[0].Drift) == 0 {
		t.Errorf("plan drift after editing applied migration = %v, want a content-hash DRIFT entry", entries[0].Drift)
	}
	found := false
	for _, d := range entries[0].Drift {
		if strings.Contains(d, "20260817000001") {
			found = true
		}
	}
	if !found {
		t.Errorf("plan drift = %v, want an entry mentioning the edited migration 20260817000001", entries[0].Drift)
	}
}

// TestPlanJSON_CurrentVersionZeroIsSerialized guards against `omitempty`
// dropping a legitimately-applied version 0 from the JSON contract (a
// squash-to-baseline migration named e.g. 00000000000000_baseline.up.sql
// applies at version 0 — not a contrived input). A consumer must be able
// to see current_version:0 in the output, not have the key vanish.
func TestPlanJSON_CurrentVersionZeroIsSerialized(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join("migrations", "00000000000000_baseline.up.sql"), []byte(`CREATE TABLE baseline (id INTEGER PRIMARY KEY);`), 0o644); err != nil {
		t.Fatal(err)
	}

	dbURL := "sqlite://" + filepath.Join(dir, "local.db")
	t.Setenv("DBTOOLS_LOCAL_URL", dbURL)
	cfg := &config.Config{
		MigrationsDir: "migrations",
		Targets:       map[string]config.Target{"local": {URLEnv: "DBTOOLS_LOCAL_URL"}},
	}
	loadConfig = func(string) (*config.Config, error) { return cfg, nil }
	t.Cleanup(func() { loadConfig = config.Load })

	if _, err := apply.Run(cfg, "local", ""); err != nil {
		t.Fatalf("apply.Run() returned error: %v", err)
	}

	planTarget = "local"
	defer func() { planTarget = "" }()

	entries := buildPlanEntries(cfg)
	if len(entries) != 1 {
		t.Fatalf("plan entries = %d, want 1", len(entries))
	}
	if !entries[0].HasVersion {
		t.Fatalf("plan has_version = false after applying version 0, want true")
	}
	if entries[0].CurrentVersion != 0 {
		t.Fatalf("plan current_version = %d, want 0", entries[0].CurrentVersion)
	}

	b, err := json.Marshal(entries[0])
	if err != nil {
		t.Fatalf("json.Marshal() returned error: %v", err)
	}
	if !strings.Contains(string(b), `"current_version":0`) {
		t.Fatalf("marshaled plan entry = %s, want it to contain \"current_version\":0 (omitempty must not drop a real version-0 result)", b)
	}
}

// TestPlanCommand_SilencesUsageOnExit2 guards against cobra's default
// usage-block dump when plan's RunE returns its documented exit-2 error
// for pending migrations — that outcome is a correct, expected result of
// a read-only preview, not a flag/argument mistake, and printing the full
// flags block trains agents/CI logs to ignore stderr.
func TestPlanCommand_SilencesUsageOnExit2(t *testing.T) {
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
	t.Setenv("DBTOOLS_LOCAL_URL", dbURL)
	cfg := &config.Config{
		MigrationsDir: "migrations",
		Targets:       map[string]config.Target{"local": {URLEnv: "DBTOOLS_LOCAL_URL"}},
	}
	loadConfig = func(string) (*config.Config, error) { return cfg, nil }
	t.Cleanup(func() { loadConfig = config.Load })
	t.Cleanup(func() { planTarget = "" })

	var stderr strings.Builder
	rootCmd.SetErr(&stderr)
	rootCmd.SetOut(&strings.Builder{})
	t.Cleanup(func() { rootCmd.SetErr(nil); rootCmd.SetOut(nil) })

	rootCmd.SetArgs([]string{"plan", "--target", "local"})
	err = rootCmd.Execute()
	if err == nil {
		t.Fatal("plan with a pending migration returned nil error, want the documented exit-2 error")
	}
	if strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("plan's exit-2 outcome printed the cobra usage block: %s", stderr.String())
	}
}

// TestPlanCommand_UnknownFlagStillPrintsUsage guards against RunE's
// exit-2 usage-silencing bleeding into cobra's own flag-parsing errors —
// SilenceUsage must only be set for the documented ExitCodeError case,
// not unconditionally on the command.
func TestPlanCommand_UnknownFlagStillPrintsUsage(t *testing.T) {
	// planCmd is a package-level singleton: an earlier test's exit-2 case
	// may have left SilenceUsage set from a prior invocation. A flag-parse
	// error never reaches RunE (where that flag gets reset per-invocation),
	// so this test must restore known state itself.
	origSilenceUsage := planCmd.SilenceUsage
	planCmd.SilenceUsage = false
	t.Cleanup(func() { planCmd.SilenceUsage = origSilenceUsage })

	// Cobra writes the error via PrintErrln (stderr) but the usage block
	// via Println (stdout) — capture both into one buffer so the
	// assertion doesn't depend on which stream cobra picks.
	var out strings.Builder
	rootCmd.SetErr(&out)
	rootCmd.SetOut(&out)
	t.Cleanup(func() { rootCmd.SetErr(nil); rootCmd.SetOut(nil) })

	rootCmd.SetArgs([]string{"plan", "--no-such-flag"})
	if err := rootCmd.Execute(); err == nil {
		t.Fatal("plan --no-such-flag returned nil error, want a flag-parsing error")
	}
	if !strings.Contains(out.String(), "Usage:") {
		t.Fatalf("plan with an invalid flag did not print usage: %s", out.String())
	}
}

func TestBuildPlanEntries_NoLedgerSetsLedgerSkippedNotDrift(t *testing.T) {
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
	t.Setenv("DBTOOLS_LOCAL_URL", dbURL)
	cfg := &config.Config{
		MigrationsDir: "migrations",
		Targets:       map[string]config.Target{"local": {URLEnv: "DBTOOLS_LOCAL_URL"}},
	}
	loadConfig = func(string) (*config.Config, error) { return cfg, nil }
	t.Cleanup(func() { loadConfig = config.Load })

	// Set up the sqlite DB: create the users table and stamp the migrate cursor,
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

	// Schema present, no dbtools ledger — the shape of a database another
	// tool migrated. There is no cursor to stamp; plan must report the
	// ledger as skipped rather than inventing drift from its absence.

	planTarget = "local"
	defer func() { planTarget = "" }()

	entries := buildPlanEntries(cfg)
	if len(entries) != 1 {
		t.Fatalf("plan entries = %d, want 1", len(entries))
	}
	e := entries[0]
	if !e.LedgerSkipped {
		t.Errorf("plan LedgerSkipped = false, want true")
	}
	if len(e.Drift) != 0 {
		t.Errorf("plan Drift = %v, want empty (no drift detected)", e.Drift)
	}
}

func TestPlanJSON_EmitFalseAndEmptyArrays(t *testing.T) {
	entry := planJSONEntry{
		Target:         "local",
		CurrentVersion: 0,
		HasVersion:     false,
		Dirty:          false,
		Pending:        []string{},
		Drift:          []string{},
		LedgerSkipped:  false,
	}
	b, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{`"has_version":false`, `"dirty":false`, `"pending":[]`, `"drift":[]`, `"ledger_skipped":false`} {
		if !strings.Contains(got, want) {
			t.Fatalf("marshaled plan entry = %s, want it to contain %s", got, want)
		}
	}
}
