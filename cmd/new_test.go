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

func TestRunNew_DerivesFromMaxExistingWhenAheadOfClock(t *testing.T) {
	// Reproduces Issue #1: existing migration is 20260820100001, clock is 2026-08-19.
	// runNew must generate 20260820100002 to avoid landing behind the tracked cursor.
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
	if err := os.WriteFile(filepath.Join("migrations", "20260820100001_initial.up.sql"), []byte("-- initial\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	clockTime := time.Date(2026, 8, 19, 17, 3, 57, 0, time.UTC)
	path, err := runNew(clockTime, "null unbacked source file names")
	if err != nil {
		t.Fatalf("runNew() returned error: %v", err)
	}

	want := filepath.Join("migrations", "20260820100002_null_unbacked_source_file_names.up.sql")
	if path != want {
		t.Errorf("runNew() path = %q, want %q", path, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("migration file %q not created: %v", want, err)
	}
}

func TestRunNew_AutomaticallyIncrementsPastExisting(t *testing.T) {
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
	firstPath, err := runNew(fixedNow, "add widget")
	if err != nil {
		t.Fatalf("first runNew() failed: %v", err)
	}
	if firstPath != filepath.Join("migrations", "20260701120000_add_widget.up.sql") {
		t.Errorf("first path = %q, want 20260701120000", firstPath)
	}

	// Calling runNew again at the exact same timestamp with another migration
	// must increment version to 20260701120001 instead of colliding at 20260701120000.
	secondPath, err := runNew(fixedNow, "add users")
	if err != nil {
		t.Fatalf("second runNew() failed: %v", err)
	}
	wantSecond := filepath.Join("migrations", "20260701120001_add_users.up.sql")
	if secondPath != wantSecond {
		t.Errorf("second path = %q, want %q", secondPath, wantSecond)
	}
	if _, err := os.Stat(wantSecond); err != nil {
		t.Errorf("second migration file %q not found on disk", wantSecond)
	}
}
