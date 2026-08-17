package migrator

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("SELECT 1;"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestListVersions(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "20260101000000_create_widgets.up.sql")
	writeFile(t, dir, "20260102000000_add_column.up.sql")
	writeFile(t, dir, "README.md") // must be ignored

	versions, err := ListVersions(dir)
	if err != nil {
		t.Fatalf("ListVersions() returned error: %v", err)
	}
	want := map[uint64]bool{20260101000000: true, 20260102000000: true}
	if len(versions) != len(want) {
		t.Fatalf("ListVersions() = %v, want 2 entries matching %v", versions, want)
	}
	for _, v := range versions {
		if !want[v] {
			t.Errorf("unexpected version %d in result", v)
		}
	}
}

func TestListVersions_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	versions, err := ListVersions(dir)
	if err != nil {
		t.Fatalf("ListVersions() returned error: %v", err)
	}
	if len(versions) != 0 {
		t.Errorf("ListVersions() = %v, want empty", versions)
	}
}
