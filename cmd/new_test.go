package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunNew_CreatesMigrationFile(t *testing.T) {
	dir := t.TempDir()
	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("dbtools.toml", []byte(`
migrations_dir = "migrations"
[targets.local]
url_env = "X"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir("migrations", 0o755); err != nil {
		t.Fatal(err)
	}

	fixedNow := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	path, err := runNew(fixedNow, "add widget")
	if err != nil {
		t.Fatalf("runNew() returned error: %v", err)
	}

	want := filepath.Join("migrations", "20260701120000_add_widget.up.sql")
	if path != want {
		t.Errorf("runNew() path = %q, want %q", path, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("migration file not created: %v", err)
	}
}
