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

func TestEnsureDatabasePostgresUnreachable(t *testing.T) {
	withFake(t, "postgres")
	// If the database host/instance is unreachable, EnsureDatabase returns nil
	// so the actual connection error can be handled by Open.
	err := EnsureDatabase(nil, "postgres://user:pass@127.0.0.1:54321/testdb?sslmode=disable")
	if err != nil {
		t.Fatalf("expected nil for unreachable postgres instance, got: %v", err)
	}
}

func TestEnsureDatabaseMSSQLUnreachable(t *testing.T) {
	withFake(t, "mssql")
	// If the database host/instance is unreachable, EnsureDatabase returns nil
	// so the actual connection error can be handled by Open.
	err := EnsureDatabase(nil, "mssql://sa:Pass123@127.0.0.1:54321?database=testdb")
	if err != nil {
		t.Fatalf("expected nil for unreachable mssql instance, got: %v", err)
	}
}
