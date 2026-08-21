//go:build integration

package mssqlengine

import (
	"os"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/testdb"
)

// A migration file with multiple GO-separated batches must apply atomically.
// Before this fix, goSplitDriver.Run delegated each batch to the inner
// golang-migrate driver independently, so an earlier batch's INSERT
// committed even though a later batch in the same file failed.
//
// The table itself is created on a separate connection, outside the
// migration under test, so this test can assert on its row count after
// Run() fails.
func TestGoSplitDriver_Run_RollsBackWholeFileOnMidBatchFailure(t *testing.T) {
	url := os.Getenv("DBTOOLS_TEST_MSSQL_URL")
	if url == "" {
		t.Skip("DBTOOLS_TEST_MSSQL_URL not set, skipping integration test")
	}
	if err := testdb.ResetTracking(url); err != nil {
		t.Fatal(err)
	}

	db, err := Open(url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("DROP TABLE IF EXISTS dbtools_test_tx_rollback"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE dbtools_test_tx_rollback (id INT)"); err != nil {
		t.Fatal(err)
	}

	driver, err := (&goSplitDriver{}).Open(url)
	if err != nil {
		t.Fatalf("Open() returned error: %v", err)
	}
	defer driver.Close()

	migration := strings.NewReader(`
INSERT INTO dbtools_test_tx_rollback (id) VALUES (1);
GO
INSERT INTO dbtools_test_tx_rollback (id) VALUES ('not-an-int');
GO
`)
	if err := driver.Run(migration); err == nil {
		t.Fatal("expected error from invalid batch 2, got nil")
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM dbtools_test_tx_rollback").Scan(&count); err != nil {
		t.Fatalf("querying row count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 rows after rollback (batch 1's INSERT should also be rolled back), got %d", count)
	}
}
