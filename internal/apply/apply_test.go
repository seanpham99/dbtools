package apply

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seanpham99/dbtools/internal/config"
	_ "github.com/seanpham99/dbtools/internal/engine/sqliteengine"
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

func TestRun_BatchTimestampMigrations(t *testing.T) {
	tmpDir := t.TempDir()
	migDir := filepath.Join(tmpDir, "migrations")
	if err := os.MkdirAll(migDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 3 migrations with non-consecutive timestamp version numbers
	os.WriteFile(filepath.Join(migDir, "20260101120000_create_users.up.sql"), []byte("CREATE TABLE users (id INTEGER PRIMARY KEY);"), 0o644)
	os.WriteFile(filepath.Join(migDir, "20260102153000_create_orders.up.sql"), []byte("CREATE TABLE orders (id INTEGER PRIMARY KEY);"), 0o644)
	os.WriteFile(filepath.Join(migDir, "20260105090000_create_items.up.sql"), []byte("CREATE TABLE items (id INTEGER PRIMARY KEY);"), 0o644)

	dbPath := filepath.Join(tmpDir, "test.db")
	t.Setenv("DBTOOLS_TEST_BATCH_URL", "sqlite://"+dbPath)

	cfg := &config.Config{
		MigrationsDir: migDir,
		Targets: map[string]config.Target{
			"local": {URLEnv: "DBTOOLS_TEST_BATCH_URL"},
		},
	}

	status, err := Run(cfg, "local", "")
	if err != nil {
		t.Fatalf("Run() failed on timestamp batch migrations: %v", err)
	}

	if !status.HasVersion || status.CurrentVersion != 20260105090000 {
		t.Errorf("CurrentVersion = %d (hasVersion=%v), want 20260105090000", status.CurrentVersion, status.HasVersion)
	}
	if len(status.Pending) != 0 {
		t.Errorf("Pending count = %d, want 0", len(status.Pending))
	}
}
