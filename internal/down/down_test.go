package down

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seanpham99/dbtools/internal/apply"
	"github.com/seanpham99/dbtools/internal/config"
	_ "github.com/seanpham99/dbtools/internal/engine/sqliteengine"
)

func TestDown_Run(t *testing.T) {
	tmpDir := t.TempDir()
	migDir := filepath.Join(tmpDir, "migrations")
	if err := os.MkdirAll(migDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 2 migrations with up and down
	os.WriteFile(filepath.Join(migDir, "20260101000000_create_users.up.sql"), []byte("CREATE TABLE users (id INTEGER PRIMARY KEY);"), 0o644)
	os.WriteFile(filepath.Join(migDir, "20260101000000_create_users.down.sql"), []byte("DROP TABLE users;"), 0o644)
	os.WriteFile(filepath.Join(migDir, "20260102000000_create_orders.up.sql"), []byte("CREATE TABLE orders (id INTEGER PRIMARY KEY);"), 0o644)
	os.WriteFile(filepath.Join(migDir, "20260102000000_create_orders.down.sql"), []byte("DROP TABLE orders;"), 0o644)

	dbPath := filepath.Join(tmpDir, "test.db")
	t.Setenv("DBTOOLS_TEST_DOWN_URL", "sqlite://"+dbPath)

	cfg := &config.Config{
		MigrationsDir: migDir,
		Targets: map[string]config.Target{
			"local": {URLEnv: "DBTOOLS_TEST_DOWN_URL"},
		},
	}

	// Up both
	if _, err := apply.Run(cfg, "local", ""); err != nil {
		t.Fatalf("apply.Run() failed: %v", err)
	}

	// Preview 1 step down
	plan, err := Preview(cfg, "local", 1, "")
	if err != nil {
		t.Fatalf("Preview() failed: %v", err)
	}
	if len(plan) != 1 || plan[0].Version != 20260102000000 {
		t.Fatalf("Preview() = %+v, want [20260102000000]", plan)
	}

	// Down 1 step
	res, err := Run(cfg, "local", 1, "")
	if err != nil {
		t.Fatalf("Run() down 1 step failed: %v", err)
	}
	if len(res.RevertedVersions) != 1 || res.RevertedVersions[0] != 20260102000000 {
		t.Errorf("RevertedVersions = %v, want [20260102000000]", res.RevertedVersions)
	}
	if !res.HasVersion || res.CurrentVersion != 20260101000000 {
		t.Errorf("CurrentVersion = %d (hasVersion=%v), want 20260101000000", res.CurrentVersion, res.HasVersion)
	}

	// Down remaining step
	res2, err := Run(cfg, "local", 1, "")
	if err != nil {
		t.Fatalf("Run() down remaining failed: %v", err)
	}
	if len(res2.RevertedVersions) != 1 || res2.RevertedVersions[0] != 20260101000000 {
		t.Errorf("RevertedVersions = %v, want [20260101000000]", res2.RevertedVersions)
	}
	if res2.HasVersion {
		t.Errorf("HasVersion = true, want false after reverting all migrations")
	}
}
