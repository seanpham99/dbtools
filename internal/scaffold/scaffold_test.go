package scaffold

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUpFilename(t *testing.T) {
	now := time.Date(2026, 7, 1, 4, 11, 34, 0, time.UTC)
	got, err := UpFilename(now, "add widget table", ".up.sql")
	if err != nil {
		t.Fatalf("UpFilename() error: %v", err)
	}
	want := "20260701041134_add_widget_table.up.sql"
	if got != want {
		t.Errorf("UpFilename() = %q, want %q", got, want)
	}
}

func TestUpFilename_AlreadySlug(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	got, err := UpFilename(now, "add_widget_table", ".up.sql")
	if err != nil {
		t.Fatalf("UpFilename() error: %v", err)
	}
	want := "20260102030405_add_widget_table.up.sql"
	if got != want {
		t.Errorf("UpFilename() = %q, want %q", got, want)
	}
}

func TestUpFilename_RejectsTraversal(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, name := range []string{"../../pwn", "a/b", "back\\slash", "line\nbreak", ".."} {
		if _, err := UpFilename(now, name, ".up.sql"); err == nil {
			t.Errorf("UpFilename(%q) accepted an unsafe name", name)
		}
	}
}

func TestNextVersion_EmptyOrNonExistentDir(t *testing.T) {
	now := time.Date(2026, 8, 19, 17, 3, 57, 0, time.UTC)

	// Non-existent directory returns clock version
	ver, err := NextVersion(now, "/non/existent/dir/path", ".up.sql")
	if err != nil {
		t.Fatalf("NextVersion() error: %v", err)
	}
	if ver != 20260819170357 {
		t.Errorf("NextVersion() = %d, want %d", ver, 20260819170357)
	}

	// Empty directory returns clock version
	tmp := t.TempDir()
	ver, err = NextVersion(now, tmp, ".up.sql")
	if err != nil {
		t.Fatalf("NextVersion() error: %v", err)
	}
	if ver != 20260819170357 {
		t.Errorf("NextVersion() = %d, want %d", ver, 20260819170357)
	}
}

func TestNextVersion_ExistingBehindClock(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "20260810100000_old_migration.up.sql"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "20260815100000_newer_migration.up.sql"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 8, 19, 17, 3, 57, 0, time.UTC)
	ver, err := NextVersion(now, tmp, ".up.sql")
	if err != nil {
		t.Fatalf("NextVersion() error: %v", err)
	}
	if ver != 20260819170357 {
		t.Errorf("NextVersion() = %d, want clock %d", ver, 20260819170357)
	}
}

func TestNextVersion_ExistingAheadOfClock(t *testing.T) {
	// Reproduces Issue #1: repo has future-dated migrations 20260820100000, 20260820100001
	// while current clock is 2026-08-19. Next version must be 20260820100002.
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "20260820100000_step1.up.sql"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "20260820100001_step2.up.sql"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "non_migration_file.txt"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 8, 19, 17, 3, 57, 0, time.UTC)
	ver, err := NextVersion(now, tmp, ".up.sql")
	if err != nil {
		t.Fatalf("NextVersion() error: %v", err)
	}
	if ver != 20260820100002 {
		t.Errorf("NextVersion() = %d, want max+1 (20260820100002)", ver)
	}

	fn, err := NextUpFilename(now, tmp, ".up.sql", "null unbacked source file names")
	if err != nil {
		t.Fatalf("NextUpFilename() error: %v", err)
	}
	wantFn := "20260820100002_null_unbacked_source_file_names.up.sql"
	if fn != wantFn {
		t.Errorf("NextUpFilename() = %q, want %q", fn, wantFn)
	}
}
