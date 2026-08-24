package engine

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDatabaseSQLite(t *testing.T) {
	withFake(t, "sqlite")
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "nested", "sub", "test.db")
	rawURL := "sqlite://" + dbPath

	if err := EnsureDatabase(nil, rawURL); err != nil {
		t.Fatalf("EnsureDatabase(sqlite) failed: %v", err)
	}

	dir := filepath.Dir(dbPath)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("expected directory %s to exist, err: %v", dir, err)
	}
}

func TestEnsureDatabaseInvalidURL(t *testing.T) {
	err := EnsureDatabase(nil, "://invalid-url")
	if err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
}
