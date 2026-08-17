package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunInit_CreatesConfigAndMigrationsDir(t *testing.T) {
	dir := t.TempDir()
	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := runInit(); err != nil {
		t.Fatalf("runInit() returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "dbtools.toml")); err != nil {
		t.Errorf("dbtools.toml not created: %v", err)
	}
	if info, err := os.Stat(filepath.Join(dir, "migrations")); err != nil || !info.IsDir() {
		t.Errorf("migrations/ dir not created: %v", err)
	}
}

func TestRunInit_DoesNotOverwriteExistingConfig(t *testing.T) {
	dir := t.TempDir()
	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	configPath := filepath.Join(dir, "dbtools.toml")
	if err := os.WriteFile(configPath, []byte("# custom\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runInit(); err != nil {
		t.Fatalf("runInit() returned error: %v", err)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# custom\n" {
		t.Errorf("existing dbtools.toml was overwritten: got %q", string(data))
	}
}
