package cmd

import (
	"testing"

	"github.com/dbtools/dbtools/internal/config"
	"github.com/dbtools/dbtools/internal/engine"
	"github.com/dbtools/dbtools/internal/statusinfo"
)

func TestRunResetRecreatesAppliesAndSeedsLocal(t *testing.T) {
	origLoadConfig := loadConfig
	origResetDatabase := resetLocalDatabase
	origApplyRun := applyRun
	origSeedRun := seedRun
	t.Cleanup(func() {
		loadConfig = origLoadConfig
		resetLocalDatabase = origResetDatabase
		applyRun = origApplyRun
		seedRun = origSeedRun
	})

	t.Setenv("DBTOOLS_LOCAL_URL", "mssql://sa:pw@localhost:14330?database=dbtools_local&TrustServerCertificate=true")

	loadConfig = func(path string) (*config.Config, error) {
		return &config.Config{
			MigrationsDir: "migrations",
			Targets: map[string]config.Target{
				"local": {URLEnv: "DBTOOLS_LOCAL_URL"},
			},
		}, nil
	}
	resetCalled := false
	resetLocalDatabase = func(engine.Engine, string) error {
		resetCalled = true
		return nil
	}
	applyCalled := false
	applyRun = func(cfg *config.Config, targetName string, _ string) (*statusinfo.Status, error) {
		applyCalled = true
		if targetName != "local" {
			t.Fatalf("applyRun called for %q, want local", targetName)
		}
		return &statusinfo.Status{Target: "local", CurrentVersion: 20260701041134, HasVersion: true}, nil
	}
	seedCalled := false
	seedRun = func(_ engine.Engine, databaseURL string) error {
		seedCalled = true
		if databaseURL != "mssql://sa:pw@localhost:14330?database=dbtools_local&TrustServerCertificate=true" {
			t.Fatalf("seedRun called with %q", databaseURL)
		}
		return nil
	}

	if err := runReset(); err != nil {
		t.Fatalf("runReset() returned error: %v", err)
	}
	if !resetCalled {
		t.Fatal("runReset() did not recreate the local database")
	}
	if !applyCalled {
		t.Fatal("runReset() did not apply migrations")
	}
	if !seedCalled {
		t.Fatal("runReset() did not run seed.sql")
	}
}
