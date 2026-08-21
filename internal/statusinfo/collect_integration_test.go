//go:build integration

package statusinfo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seanpham99/dbtools/internal/testdb"
)

func TestCollect(t *testing.T) {
	url := os.Getenv("DBTOOLS_TEST_MSSQL_URL")
	if url == "" {
		t.Skip("DBTOOLS_TEST_MSSQL_URL not set, skipping integration test")
	}

	if err := testdb.ResetTracking(url); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "20260101000000_create_widgets.up.sql"),
		[]byte("CREATE TABLE dbtools_test_collect (id INT PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "20260102000000_add_index.up.sql"),
		[]byte("CREATE INDEX ix_dbtools_test_collect ON dbtools_test_collect (id);"), 0o644); err != nil {
		t.Fatal(err)
	}

	status, err := Collect(url, dir, "local")
	if err != nil {
		t.Fatalf("Collect() before any Up() returned error: %v", err)
	}
	if len(status.Pending) != 2 {
		t.Errorf("expected 2 pending before Up(), got %d: %v", len(status.Pending), status.Pending)
	}
}
