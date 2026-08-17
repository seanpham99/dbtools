package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dbtools/dbtools/internal/config"
)

// A configured engine that contradicts the target URL's scheme must be
// rejected before any connection attempt. The URL below points at a
// non-routable host — if validation ever slipped past and dialed, the
// tests would hang/fail on connection instead of returning the
// mismatch error immediately.

func mismatchConfig(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv("DBTOOLS_GUARD_TEST_URL", "mssql://sa:x@192.0.2.1:1433?database=x")
	return &config.Config{
		MigrationsDir: t.TempDir(),
		Targets: map[string]config.Target{
			"local": {URLEnv: "DBTOOLS_GUARD_TEST_URL", Engine: "postgres"},
		},
	}
}

func TestCollectStatuses_RejectsEngineSchemeMismatch(t *testing.T) {
	cfg := mismatchConfig(t)
	statuses, failures := collectStatuses(cfg)
	if len(statuses) != 0 {
		t.Fatalf("expected no statuses, got %+v", statuses)
	}
	if len(failures) != 1 {
		t.Fatalf("expected 1 failure, got %+v", failures)
	}
	if !strings.Contains(failures[0].Error, "does not match") {
		t.Fatalf("failure should name the engine/scheme mismatch, got: %s", failures[0].Error)
	}
}

func TestRunPush_RejectsEngineSchemeMismatch(t *testing.T) {
	cfg := mismatchConfig(t)
	dir := t.TempDir()
	raw := `migrations_dir = "` + cfg.MigrationsDir + `"

[targets.local]
url_env = "DBTOOLS_GUARD_TEST_URL"
engine = "postgres"
`
	if err := os.WriteFile(filepath.Join(dir, "dbtools.toml"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	err := runPush("local")
	if err == nil {
		t.Fatal("runPush() should fail on engine/scheme mismatch")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("unexpected error: %v", err)
	}
}
