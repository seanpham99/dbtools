//go:build integration

package ddlcheck

import (
	"os"
	"testing"

	"github.com/dbtools/dbtools/internal/dbconn"
)

func TestExists(t *testing.T) {
	url := os.Getenv("DBTOOLS_TEST_MSSQL_URL")
	if url == "" {
		t.Skip("DBTOOLS_TEST_MSSQL_URL not set, skipping integration test")
	}

	db, err := dbconn.Open(url)
	if err != nil {
		t.Fatalf("dbconn.Open() returned error: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec("DROP TABLE IF EXISTS dbtools_test_ddlcheck_widgets"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE dbtools_test_ddlcheck_widgets (id INT PRIMARY KEY)"); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP TABLE dbtools_test_ddlcheck_widgets")

	exists, err := Exists(db, ObjectRef{Schema: "dbo", Name: "dbtools_test_ddlcheck_widgets", Kind: "table"})
	if err != nil {
		t.Fatalf("Exists() returned error: %v", err)
	}
	if !exists {
		t.Error("Exists() = false, want true for a table that was just created")
	}

	exists, err = Exists(db, ObjectRef{Schema: "dbo", Name: "dbtools_test_ddlcheck_missing_table", Kind: "table"})
	if err != nil {
		t.Fatalf("Exists() returned error: %v", err)
	}
	if exists {
		t.Error("Exists() = true, want false for a table that was never created")
	}
}

func TestExists_UnknownKind(t *testing.T) {
	url := os.Getenv("DBTOOLS_TEST_MSSQL_URL")
	if url == "" {
		t.Skip("DBTOOLS_TEST_MSSQL_URL not set, skipping integration test")
	}
	db, err := dbconn.Open(url)
	if err != nil {
		t.Fatalf("dbconn.Open() returned error: %v", err)
	}
	defer db.Close()

	if _, err := Exists(db, ObjectRef{Schema: "dbo", Name: "x", Kind: "index"}); err == nil {
		t.Fatal("expected error for unknown Kind, got nil")
	}
}
