//go:build integration

package seed

import (
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/microsoft/go-mssqldb"

	"github.com/dbtools/dbtools/internal/engine/mssqlengine"
)

func TestRun_ExecutesSeedFile(t *testing.T) {
	rawURL := os.Getenv("DBTOOLS_TEST_MSSQL_URL")
	if rawURL == "" {
		t.Skip("DBTOOLS_TEST_MSSQL_URL not set, skipping integration test")
	}

	dir := t.TempDir()
	oldwd, _ := os.Getwd()
	defer os.Chdir(oldwd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// go-mssqldb's DSN parser only recognizes "sqlserver://" as its
	// URL-style prefix; dbtools connection strings use "mssql://".
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	u.Scheme = "sqlserver"

	db, err := sql.Open("sqlserver", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("IF OBJECT_ID('dbtools_test_seed') IS NOT NULL DROP TABLE dbtools_test_seed; CREATE TABLE dbtools_test_seed (id INT PRIMARY KEY);"); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	seedContent := "INSERT INTO dbtools_test_seed (id) VALUES (1);\nGO\nINSERT INTO dbtools_test_seed (id) VALUES (2);\n"
	if err := os.WriteFile(filepath.Join(dir, Filename), []byte(seedContent), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Run(mssqlengine.MSSQL{}, rawURL); err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM dbtools_test_seed").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("expected 2 rows after Run(), got %d", count)
	}
}
