package migrator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLint_CleanDirectory(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "20260101000000_create_users.up.sql"), []byte("CREATE TABLE users (id INT);"), 0o644)
	os.WriteFile(filepath.Join(tmp, "20260102000000_add_orders.up.sql"), []byte("CREATE TABLE orders (id INT);"), 0o644)
	os.WriteFile(filepath.Join(tmp, ".gitkeep"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(tmp, "README.md"), []byte("Migrations notes"), 0o644)

	report, err := Lint(tmp)
	if err != nil {
		t.Fatalf("Lint() error: %v", err)
	}
	if report.HasErrors() {
		t.Errorf("Lint() findings = %+v, want none", report.Findings)
	}
	if report.Total != 2 {
		t.Errorf("Lint() total = %d, want 2", report.Total)
	}
}

func TestLint_DuplicateVersions(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "20260820100002_rebuild_trading.up.sql"), []byte("SELECT 1;"), 0o644)
	os.WriteFile(filepath.Join(tmp, "20260820100002_add_indexes.up.sql"), []byte("SELECT 2;"), 0o644)

	report, err := Lint(tmp)
	if err != nil {
		t.Fatalf("Lint() error: %v", err)
	}
	if !report.HasErrors() {
		t.Fatal("Lint() expected errors for duplicate versions, got none")
	}

	foundDup := false
	for _, f := range report.Findings {
		if f.Rule == "duplicate-version-number" {
			foundDup = true
		}
	}
	if !foundDup {
		t.Errorf("Lint() findings = %+v, want duplicate-version-number", report.Findings)
	}
}

func TestLint_InvalidNamesAndEmptyFiles(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "invalid_no_version.sql"), []byte("SELECT 1;"), 0o644)
	os.WriteFile(filepath.Join(tmp, "20260101000000_empty.up.sql"), []byte("   \n\t  "), 0o644)

	report, err := Lint(tmp)
	if err != nil {
		t.Fatalf("Lint() error: %v", err)
	}
	if len(report.Findings) != 2 {
		t.Fatalf("Lint() findings count = %d, want 2 (%+v)", len(report.Findings), report.Findings)
	}
}
