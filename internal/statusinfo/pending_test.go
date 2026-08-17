package statusinfo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListMigrationFiles(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{
		"20260101000000_create_widgets.up.sql",
		"20260102000000_add_index.up.sql",
		"README.md", // non-migration file, must be ignored
	} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("-- x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := ListMigrationFiles(dir)
	if err != nil {
		t.Fatalf("ListMigrationFiles() returned error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("ListMigrationFiles() returned %d files, want 2: %+v", len(files), files)
	}
	if files[0].Version != 20260101000000 || files[1].Version != 20260102000000 {
		t.Errorf("unexpected versions: %+v", files)
	}
}

func TestComputePending_NoVersionAppliedYet(t *testing.T) {
	all := []MigrationFile{
		{Version: 20260101000000, Filename: "20260101000000_a.up.sql"},
		{Version: 20260102000000, Filename: "20260102000000_b.up.sql"},
	}
	got := ComputePending(0, false, all)
	if len(got) != 2 {
		t.Fatalf("ComputePending() = %v, want both files pending", got)
	}
}

func TestComputePending_SomeApplied(t *testing.T) {
	all := []MigrationFile{
		{Version: 20260101000000, Filename: "20260101000000_a.up.sql"},
		{Version: 20260102000000, Filename: "20260102000000_b.up.sql"},
		{Version: 20260103000000, Filename: "20260103000000_c.up.sql"},
	}
	got := ComputePending(20260102000000, true, all)
	want := []string{"20260103000000_c.up.sql"}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("ComputePending() = %v, want %v", got, want)
	}
}

func TestComputePending_AllApplied(t *testing.T) {
	all := []MigrationFile{
		{Version: 20260101000000, Filename: "20260101000000_a.up.sql"},
	}
	got := ComputePending(20260101000000, true, all)
	if len(got) != 0 {
		t.Errorf("ComputePending() = %v, want empty", got)
	}
}

func TestComputePendingVersions_SomeApplied(t *testing.T) {
	all := []MigrationFile{
		{Version: 20260101000000, Filename: "20260101000000_a.up.sql"},
		{Version: 20260102000000, Filename: "20260102000000_b.up.sql"},
		{Version: 20260103000000, Filename: "20260103000000_c.up.sql"},
	}
	got := ComputePendingVersions(20260102000000, true, all)
	want := []uint64{20260103000000}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("ComputePendingVersions() = %v, want %v", got, want)
	}
}

func TestComputePendingVersions_NoVersionAppliedYet(t *testing.T) {
	all := []MigrationFile{
		{Version: 20260101000000, Filename: "20260101000000_a.up.sql"},
	}
	got := ComputePendingVersions(0, false, all)
	if len(got) != 1 || got[0] != 20260101000000 {
		t.Errorf("ComputePendingVersions() = %v, want [20260101000000]", got)
	}
}
