package migrator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindMigrationFile(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"20260101000000_create_widgets.up.sql",
		"20260102000000_add_index.up.sql",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("SELECT 1;"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got, err := FindMigrationFile(dir, 20260102000000)
	if err != nil {
		t.Fatalf("FindMigrationFile() returned error: %v", err)
	}
	if got != "20260102000000_add_index.up.sql" {
		t.Errorf("FindMigrationFile() = %q, want %q", got, "20260102000000_add_index.up.sql")
	}
}

func TestFindMigrationFile_NotFound(t *testing.T) {
	dir := t.TempDir()
	if _, err := FindMigrationFile(dir, 99999999999999); err == nil {
		t.Fatal("expected error for version with no matching migration file, got nil")
	}
}
