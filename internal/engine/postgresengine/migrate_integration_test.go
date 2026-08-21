//go:build integration

package postgresengine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seanpham99/dbtools/internal/migrator"
	"github.com/seanpham99/dbtools/internal/seed"
)

// TestLiveMigrateUpAndSeed exercises the full golang-migrate path (Open,
// Up, Version) and seed execution against a real Postgres server.
func TestLiveMigrateUpAndSeed(t *testing.T) {
	rawURL := os.Getenv("DBTOOLS_TEST_POSTGRES_URL")
	if rawURL == "" {
		t.Skip("DBTOOLS_TEST_POSTGRES_URL not set, skipping integration test")
	}

	eng := Postgres{}
	db, err := eng.Open(rawURL)
	if err != nil {
		t.Fatalf("Open() returned error: %v", err)
	}
	defer db.Close()
	t.Cleanup(func() {
		db.Exec(`DROP TABLE IF EXISTS pg_it_widgets`)
		db.Exec(`DROP TABLE IF EXISTS schema_migrations`)
	})

	dir := t.TempDir()
	up := `CREATE TABLE pg_it_widgets (id bigint PRIMARY KEY, label text NOT NULL);
CREATE OR REPLACE FUNCTION pg_it_touch() RETURNS trigger AS $fn1$ BEGIN RETURN NEW; END; $fn1$ LANGUAGE plpgsql;`
	down := `DROP FUNCTION pg_it_touch; DROP TABLE pg_it_widgets;`
	if err := os.WriteFile(filepath.Join(dir, "20260817000001_widgets.up.sql"), []byte(up), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "20260817000001_widgets.down.sql"), []byte(down), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec(`DROP FUNCTION IF EXISTS pg_it_touch`) })

	m, err := migrator.Open(rawURL, dir)
	if err != nil {
		t.Fatalf("migrator.Open() returned error: %v", err)
	}
	defer m.Close()

	applied, err := m.Up()
	if err != nil {
		t.Fatalf("Up() returned error: %v", err)
	}
	if !applied {
		t.Fatal("Up() = false, want an applied migration")
	}
	version, dirty, hasVersion, err := m.Version()
	if err != nil || dirty || !hasVersion || version != 20260817000001 {
		t.Fatalf("Version() = %d dirty=%v has=%v err=%v", version, dirty, hasVersion, err)
	}

	// Seed through the engine seam from a temp working directory.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd) })
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(seed.Filename, []byte(`INSERT INTO pg_it_widgets (id, label) VALUES (1, 'a'), (2, 'b');`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := seed.Run(eng, rawURL); err != nil {
		t.Fatalf("seed.Run() returned error: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pg_it_widgets`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("widgets count = %d, err = %v; want 2", count, err)
	}
}
