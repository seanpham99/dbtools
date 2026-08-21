package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/apply"
	"github.com/seanpham99/dbtools/internal/config"
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
