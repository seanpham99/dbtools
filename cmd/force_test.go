package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seanpham99/dbtools/internal/config"
	_ "github.com/seanpham99/dbtools/internal/engine/sqliteengine"
)

func TestRunForceRefusesWithoutYes(t *testing.T) {
	origForceYes := forceYes
	t.Cleanup(func() { forceYes = origForceYes })
	forceYes = false

	rootCmd.SetArgs([]string{"force", "20260101000000"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected error running force without --yes, got nil")
	}
}

func TestRunForceRefusesProtectedTarget(t *testing.T) {
	origLoadConfig := loadConfig
	origForceYes := forceYes
	t.Cleanup(func() {
		loadConfig = origLoadConfig
		forceYes = origForceYes
	})
	forceYes = true

	loadConfig = func(path string) (*config.Config, error) {
		return &config.Config{
			MigrationsDir: "migrations",
			Targets: map[string]config.Target{
				"prod": {URLEnv: "DBTOOLS_PROD_URL", Protected: true},
			},
		}, nil
	}

	err := runForce("prod", 20260101000000)
	if err == nil {
		t.Fatal("expected error running force against protected target, got nil")
	}
}

func TestRunForceSuccessSQLite(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "force_test.db")
	migrationsDir := filepath.Join(tmpDir, "migrations")
	if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
		t.Fatalf("creating migrations dir: %v", err)
	}

	migSQL := "CREATE TABLE items (id INTEGER PRIMARY KEY);"
	if err := os.WriteFile(filepath.Join(migrationsDir, "20260101000000_init.up.sql"), []byte(migSQL), 0o644); err != nil {
		t.Fatalf("writing migration file: %v", err)
	}

	origLoadConfig := loadConfig
	origForceYes := forceYes
	t.Cleanup(func() {
		loadConfig = origLoadConfig
		forceYes = origForceYes
	})
	forceYes = true

	t.Setenv("DBTOOLS_TEST_FORCE_URL", "sqlite://"+dbPath)

	loadConfig = func(path string) (*config.Config, error) {
		return &config.Config{
			MigrationsDir: migrationsDir,
			Targets: map[string]config.Target{
				"local": {URLEnv: "DBTOOLS_TEST_FORCE_URL"},
			},
		}, nil
	}

	if err := runForce("local", 20260101000000); err != nil {
		t.Fatalf("runForce(local, 20260101000000) returned error: %v", err)
	}
}
