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

// TestRun_RefusesCustomUpSuffix is a regression test for a review finding:
// golang-migrate's own file source is hardcoded to ".up.sql"/".down.sql"
// regardless of migrations.up_suffix, so a custom suffix would make
// dir.PendingAfter report files as pending that golang-migrate can never
// resolve — applying silently nothing while looking like success. Run
// must refuse instead of no-op'ing.
func TestRun_RefusesCustomUpSuffix(t *testing.T) {
	cfg := &config.Config{
		MigrationsDir: "migrations",
		Migrations:    config.MigrationsConfig{UpSuffix: ".sql"},
		Targets:       map[string]config.Target{"staging": {URLEnv: "DBTOOLS_APPLY_TEST_UNSET_SUFFIX"}},
	}
	t.Setenv("DBTOOLS_APPLY_TEST_UNSET_SUFFIX", "sqlite://"+filepath.Join(t.TempDir(), "test.db"))
	_, err := Run(cfg, "staging", "")
	if err == nil {
		t.Fatal("Run() with custom up_suffix: want error, got nil")
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

// TestRun_AppliesPendingWhenCursorFileIsMissing is the regression for the
// silent-skip bug: once an already-applied migration file is renamed or
// deleted from migrationsDir, golang-migrate's Steps(1) refuses to
// advance (its versionExists check rejects the cursor version), and the
// old Step wrapper swallowed that os.ErrNotExist as "no change" — `up`
// reported success while every pending file stayed pending. Run must
// still apply the pending file: the ledger cursor, not the on-disk file
// set, is the record of what was applied.
func TestRun_AppliesPendingWhenCursorFileIsMissing(t *testing.T) {
	tmpDir := t.TempDir()
	migDir := filepath.Join(tmpDir, "migrations")
	if err := os.MkdirAll(migDir, 0o755); err != nil {
		t.Fatal(err)
	}

	v1 := filepath.Join(migDir, "20260101120000_create_users.up.sql")
	if err := os.WriteFile(v1, []byte("CREATE TABLE users (id INTEGER PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	t.Setenv("DBTOOLS_TEST_MISSING_CURSOR_URL", "sqlite://"+dbPath)
	cfg := &config.Config{
		MigrationsDir: migDir,
		Targets: map[string]config.Target{
			"local": {URLEnv: "DBTOOLS_TEST_MISSING_CURSOR_URL"},
		},
	}

	status, err := Run(cfg, "local", "")
	if err != nil {
		t.Fatalf("Run() first apply failed: %v", err)
	}
	if status.CurrentVersion != 20260101120000 {
		t.Fatalf("CurrentVersion = %d, want 20260101120000", status.CurrentVersion)
	}

	// Remove the applied file and add a newer pending one — the reported
	// state where `up` used to print "now at version <old> (1 pending)".
	if err := os.Remove(v1); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(migDir, "20260102120000_add_orders.up.sql"), []byte("CREATE TABLE orders (id INTEGER PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}

	status, err = Run(cfg, "local", "")
	if err != nil {
		t.Fatalf("Run() with missing cursor file returned error: %v", err)
	}
	if status.CurrentVersion != 20260102120000 {
		t.Fatalf("CurrentVersion = %d, want 20260102120000 (pending migration must apply even when the cursor's file is gone)", status.CurrentVersion)
	}
	if len(status.Pending) != 0 {
		t.Fatalf("Pending = %v, want none", status.Pending)
	}
}
