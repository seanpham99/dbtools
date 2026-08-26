package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/apply"
	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/verify"
)

// TestSQLiteFullLoop exercises the whole command loop — up, ledger,
// verify, reset (file recreate + replay + seed) — against a real
// temp-file SQLite database. SQLite needs no server, so this is an
// ordinary unit test.
func TestSQLiteFullLoop(t *testing.T) {
	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll("migrations", 0o755); err != nil {
		t.Fatal(err)
	}
	up := `CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL);
CREATE VIEW active_users AS SELECT * FROM users;`
	if err := os.WriteFile(filepath.Join("migrations", "20260817000001_users.up.sql"), []byte(up), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("migrations", "20260817000001_users.down.sql"), []byte("DROP VIEW active_users;\nDROP TABLE users;"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("seed.sql", []byte(`INSERT INTO users (id, email) VALUES (1, 'a@example.com');`), 0o644); err != nil {
		t.Fatal(err)
	}

	dbURL := "sqlite://" + filepath.Join(dir, "local.db")
	t.Setenv("DBTOOLS_LOCAL_URL", dbURL)
	cfg := &config.Config{
		MigrationsDir: "migrations",
		Targets:       map[string]config.Target{"local": {URLEnv: "DBTOOLS_LOCAL_URL"}},
	}

	// up
	status, err := apply.Run(cfg, "local", "")
	if err != nil {
		t.Fatalf("apply.Run() returned error: %v", err)
	}
	if status.CurrentVersion != 20260817000001 {
		t.Fatalf("CurrentVersion = %d, want 20260817000001", status.CurrentVersion)
	}

	eng, err := engine.ForTarget("", dbURL)
	if err != nil {
		t.Fatal(err)
	}
	db, err := eng.Open(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// verify: clean after up
	report, err := verify.Collect(db, eng, "migrations", cfg.Migrations.UpSuffix, cfg.Ledger.Table, "local")
	if err != nil {
		t.Fatalf("verify.Collect() returned error: %v", err)
	}
	for _, e := range report.Entries {
		if e.Status != "OK" {
			t.Errorf("verify entry %d = %s (%s), want OK", e.Version, e.Status, e.Detail)
		}
	}

	// verify: dropping a created table out-of-band is reported as drift
	if _, err := db.Exec(`DROP VIEW active_users`); err != nil {
		t.Fatal(err)
	}
	report, err = verify.Collect(db, eng, "migrations", cfg.Migrations.UpSuffix, cfg.Ledger.Table, "local")
	if err != nil {
		t.Fatalf("verify.Collect() after drop returned error: %v", err)
	}
	drifted := false
	for _, e := range report.Entries {
		if e.Status == "DRIFT" && strings.Contains(e.Detail, "active_users") {
			drifted = true
		}
	}
	if !drifted {
		t.Fatalf("verify after out-of-band DROP VIEW = %+v, want a DRIFT entry naming active_users", report.Entries)
	}

	// reset: file recreated, migrations replayed, seed applied
	loadConfig = func(string) (*config.Config, error) { return cfg, nil }
	t.Cleanup(func() { loadConfig = config.Load })
	if err := runReset(); err != nil {
		t.Fatalf("runReset() returned error: %v", err)
	}

	db2, err := eng.Open(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	var email string
	if err := db2.QueryRow(`SELECT email FROM users WHERE id = 1`).Scan(&email); err != nil || email != "a@example.com" {
		t.Fatalf("seeded email = %q, err = %v; want a@example.com", email, err)
	}
	var viewCount int
	if err := db2.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'view' AND name = 'active_users'`).Scan(&viewCount); err != nil || viewCount != 1 {
		t.Fatalf("active_users view count = %d, err = %v; want 1 after replay", viewCount, err)
	}

	// generate-style introspection over the reset database
	tables, unmapped, err := eng.Introspect(db2, []string{"dbtools_migration_history", "schema_migrations"})
	if err != nil {
		t.Fatalf("Introspect() returned error: %v", err)
	}
	if len(unmapped) != 0 {
		t.Errorf("unmapped = %v, want none", unmapped)
	}
	if len(tables) != 1 || tables[0].Name != "users" {
		t.Fatalf("Introspect() tables = %+v, want just users", tables)
	}

	// down: revert migration 20260817000001
	downYes = true
	downPreview = false
	downURL = ""
	if err := runDown("local", 1); err != nil {
		t.Fatalf("runDown() returned error: %v", err)
	}

	// verify: after down, table is gone and ledger records reverted (not drift)
	report, err = verify.Collect(db2, eng, "migrations", cfg.Migrations.UpSuffix, cfg.Ledger.Table, "local")
	if err != nil {
		t.Fatalf("verify.Collect() after down returned error: %v", err)
	}
	for _, e := range report.Entries {
		if e.Status != "OK" {
			t.Errorf("verify entry %d after down = %s (%s), want OK", e.Version, e.Status, e.Detail)
		}
	}

	// re-apply up for rollback test
	if _, err := apply.Run(cfg, "local", ""); err != nil {
		t.Fatalf("apply.Run() failed: %v", err)
	}

	// rollback: soft-revert in ledger without dropping table
	rollbackYes = true
	rollbackURL = ""
	if err := runRollback("local", 1); err != nil {
		t.Fatalf("runRollback() returned error: %v", err)
	}

	// start/stop are graceful no-ops
	if err := runStart(); err != nil {
		t.Errorf("runStart() returned error: %v", err)
	}
	if err := runStop(); err != nil {
		t.Errorf("runStop() returned error: %v", err)
	}
}
