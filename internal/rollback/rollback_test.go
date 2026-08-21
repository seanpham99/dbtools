package rollback

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seanpham99/dbtools/internal/apply"
	"github.com/seanpham99/dbtools/internal/config"
	_ "github.com/seanpham99/dbtools/internal/engine/sqliteengine"
)

func TestRollback_Run(t *testing.T) {
	tmpDir := t.TempDir()
	migDir := filepath.Join(tmpDir, "migrations")
	if err := os.MkdirAll(migDir, 0o755); err != nil {
		t.Fatal(err)
	}

	os.WriteFile(filepath.Join(migDir, "20260101000000_create_users.up.sql"), []byte("CREATE TABLE users (id INTEGER PRIMARY KEY);"), 0o644)
	os.WriteFile(filepath.Join(migDir, "20260102000000_create_orders.up.sql"), []byte("CREATE TABLE orders (id INTEGER PRIMARY KEY);"), 0o644)

	dbPath := filepath.Join(tmpDir, "test.db")
	t.Setenv("DBTOOLS_TEST_ROLLBACK_URL", "sqlite://"+dbPath)

	cfg := &config.Config{
		MigrationsDir: migDir,
		Targets: map[string]config.Target{
			"local": {URLEnv: "DBTOOLS_TEST_ROLLBACK_URL"},
		},
	}

	// Up both
	if _, err := apply.Run(cfg, "local", ""); err != nil {
		t.Fatalf("apply.Run() failed: %v", err)
	}

	// Soft-revert latest version
	res, err := Run(cfg, "local", 1, "")
	if err != nil {
		t.Fatalf("Run() rollback failed: %v", err)
	}

	if len(res.RevertedVersions) != 1 || res.RevertedVersions[0] != 20260102000000 {
		t.Errorf("RevertedVersions = %v, want [20260102000000]", res.RevertedVersions)
	}
	if !res.HasCursor || res.NewCursor != 20260101000000 {
		t.Errorf("NewCursor = %d (hasCursor=%v), want 20260101000000", res.NewCursor, res.HasCursor)
	}
}
