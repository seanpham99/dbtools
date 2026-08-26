package diff_test

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/seanpham99/dbtools/internal/apply"
	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/diff"
	_ "github.com/seanpham99/dbtools/internal/engine/sqliteengine"
	_ "modernc.org/sqlite"
)

func setupDiffTestEnv(t *testing.T) (string, string, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "target.db")
	rawURL := fmt.Sprintf("sqlite://%s", dbPath)
	migrationsDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	m1Up := `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);`
	m1Down := `DROP TABLE users;`
	if err := os.WriteFile(filepath.Join(migrationsDir, "20260822000001_users.up.sql"), []byte(m1Up), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(migrationsDir, "20260822000001_users.down.sql"), []byte(m1Down), 0o644); err != nil {
		t.Fatal(err)
	}

	cfgContent := fmt.Sprintf(`migrations_dir = %q
[targets.testdb]
url_env = "DBTOOLS_TEST_DIFF_URL"
engine = "sqlite"
protected = false
`, migrationsDir)

	configPath := filepath.Join(dir, "dbtools.toml")
	if err := os.WriteFile(configPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("DBTOOLS_TEST_DIFF_URL", rawURL)

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}

	return dir, dbPath, cfg
}

func TestRun_CleanReplayMatchesTarget(t *testing.T) {
	_, _, cfg := setupDiffTestEnv(t)

	// Apply migration to target
	if _, err := apply.Run(cfg, "testdb", ""); err != nil {
		t.Fatalf("apply.Run failed: %v", err)
	}

	findings, notes, err := diff.Run(cfg, "testdb", "")
	if err != nil {
		t.Fatalf("diff.Run failed: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want 0", findings)
	}
	if len(notes) != 0 {
		t.Fatalf("notes = %+v, want 0", notes)
	}
}

func TestRun_ExtraColumnInTargetReportsExtra(t *testing.T) {
	_, dbPath, cfg := setupDiffTestEnv(t)

	// Apply migration to target
	if _, err := apply.Run(cfg, "testdb", ""); err != nil {
		t.Fatalf("apply.Run failed: %v", err)
	}

	// Manually add extra column to target
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec("ALTER TABLE users ADD COLUMN hotfix_col TEXT;"); err != nil {
		t.Fatalf("ALTER TABLE failed: %v", err)
	}

	findings, _, err := diff.Run(cfg, "testdb", "")
	if err != nil {
		t.Fatalf("diff.Run failed: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("findings count = %d, want 1; findings: %+v", len(findings), findings)
	}
	if findings[0].Kind != diff.KindExtra || findings[0].Object != diff.ObjectColumn || findings[0].Name != "hotfix_col" {
		t.Fatalf("unexpected finding: %+v, want EXTRA column hotfix_col", findings[0])
	}
}

func TestRun_AgainstFlagSkipsAutomaticProvisioning(t *testing.T) {
	dir, _, cfg := setupDiffTestEnv(t)

	// Apply migration to target
	if _, err := apply.Run(cfg, "testdb", ""); err != nil {
		t.Fatalf("apply.Run failed: %v", err)
	}

	scratchPath := filepath.Join(dir, "scratch.db")
	scratchURL := "sqlite://" + scratchPath

	findings, _, err := diff.Run(cfg, "testdb", scratchURL)
	if err != nil {
		t.Fatalf("diff.Run with againstURL failed: %v", err)
	}
	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want 0", findings)
	}

	// Verify scratch file was created and used
	if _, err := os.Stat(scratchPath); err != nil {
		t.Fatalf("expected scratch db file to exist at %s: %v", scratchPath, err)
	}
}
