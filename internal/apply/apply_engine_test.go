package apply

import (
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/config"
	// Register the MSSQL engine, as cmd/root.go does for the CLI.
	_ "github.com/seanpham99/dbtools/internal/engine/mssqlengine"
)

// A target whose configured engine contradicts its URL scheme must be
// rejected before any connection is attempted.
func TestRun_RejectsEngineSchemeMismatch(t *testing.T) {
	t.Setenv("DBTOOLS_ENGINE_MISMATCH_URL", "mssql://sa:x@127.0.0.1:1433?database=x")
	cfg := &config.Config{
		MigrationsDir: t.TempDir(),
		Targets: map[string]config.Target{
			"local": {URLEnv: "DBTOOLS_ENGINE_MISMATCH_URL", Engine: "postgres"},
		},
	}
	_, err := Run(cfg, "local", "")
	if err == nil {
		t.Fatal("Run() should fail when configured engine mismatches URL scheme")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// A URL whose scheme has no registered engine must fail with an
// actionable "unknown engine" error.
func TestRun_RejectsUnknownScheme(t *testing.T) {
	t.Setenv("DBTOOLS_ENGINE_UNKNOWN_URL", "oracle://scott:tiger@db:1521/xe")
	cfg := &config.Config{
		MigrationsDir: t.TempDir(),
		Targets: map[string]config.Target{
			"local": {URLEnv: "DBTOOLS_ENGINE_UNKNOWN_URL"},
		},
	}
	_, err := Run(cfg, "local", "")
	if err == nil {
		t.Fatal("Run() should fail for a URL scheme with no registered engine")
	}
	if !strings.Contains(err.Error(), "unknown engine") {
		t.Fatalf("unexpected error: %v", err)
	}
}
