package apply

import (
	"testing"

	"github.com/dbtools/dbtools/internal/config"
)

func TestRun_UnknownTarget(t *testing.T) {
	cfg := &config.Config{MigrationsDir: "migrations", Targets: map[string]config.Target{}}
	_, err := Run(cfg, "staging", "")
	if err == nil {
		t.Fatal("expected error for unknown target, got nil")
	}
}

func TestRun_EnvVarNotSet(t *testing.T) {
	cfg := &config.Config{
		MigrationsDir: "migrations",
		Targets:       map[string]config.Target{"staging": {URLEnv: "DBTOOLS_APPLY_TEST_UNSET"}},
	}
	_, err := Run(cfg, "staging", "")
	if err == nil {
		t.Fatal("expected error for unset env var, got nil")
	}
}
