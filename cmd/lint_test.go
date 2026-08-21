package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seanpham99/dbtools/internal/migrator"
)

func TestLint_Integration(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "20260101000000_init.up.sql"), []byte("CREATE TABLE test (id INT);"), 0o644)
	os.WriteFile(filepath.Join(dir, "20260102000000_update.up.sql"), []byte("ALTER TABLE test ADD col INT;"), 0o644)

	report, err := migrator.Lint(dir)
	if err != nil {
		t.Fatalf("migrator.Lint() returned error: %v", err)
	}
	if report.HasErrors() {
		t.Errorf("expected no errors, got %+v", report.Findings)
	}
	if report.Total != 2 {
		t.Errorf("expected 2 files checked, got %d", report.Total)
	}
}
