//go:build integration

package mssqlengine

import (
	"context"
	"os"
	"testing"
)

// GO batch splitting and whole-file rollback are the two behaviours that
// make SQL Server migrations work, and both moved from the deleted
// golang-migrate driver wrapper into ExecMigration. The tests that covered
// them went with the wrapper, so they are re-established here against the
// real thing.
func TestIntegrationExecMigration_SplitsGOBatches(t *testing.T) {
	rawURL := os.Getenv("DBTOOLS_TEST_MSSQL_URL")
	if rawURL == "" {
		t.Skip("DBTOOLS_TEST_MSSQL_URL not set, skipping integration test")
	}
	db, err := Open(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	t.Cleanup(func() {
		db.Exec(`DROP TABLE IF EXISTS dbtools_go_batch_a`)
		db.Exec(`DROP TABLE IF EXISTS dbtools_go_batch_b`)
	})
	db.Exec(`DROP TABLE IF EXISTS dbtools_go_batch_a`)
	db.Exec(`DROP TABLE IF EXISTS dbtools_go_batch_b`)

	// CREATE TABLE cannot share a batch with later statements that
	// reference it, which is the entire reason GO exists — send this
	// unsplit and SQL Server rejects it.
	migration := `CREATE TABLE dbtools_go_batch_a (id INT PRIMARY KEY);
GO
CREATE TABLE dbtools_go_batch_b (id INT PRIMARY KEY);
GO
INSERT INTO dbtools_go_batch_a (id) VALUES (1);`

	if err := (MSSQL{}).ExecMigration(ctx, conn, migration); err != nil {
		t.Fatalf("ExecMigration with GO batches: %v", err)
	}

	for _, table := range []string{"dbtools_go_batch_a", "dbtools_go_batch_b"} {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM sys.tables WHERE name = @p1`, table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("%s was not created; the GO batch did not run", table)
		}
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM dbtools_go_batch_a`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("row count = %d, want 1 — the batch after the second GO did not run", rows)
	}
}

// SQL Server has transactional DDL, so a file that fails partway must leave
// nothing behind. This is the strongest recovery guarantee of any engine
// dbtools supports and is worth pinning: without the wrapping transaction,
// the first batch would stay committed.
func TestIntegrationExecMigration_RollsBackTheWholeFile(t *testing.T) {
	rawURL := os.Getenv("DBTOOLS_TEST_MSSQL_URL")
	if rawURL == "" {
		t.Skip("DBTOOLS_TEST_MSSQL_URL not set, skipping integration test")
	}
	db, err := Open(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	t.Cleanup(func() { db.Exec(`DROP TABLE IF EXISTS dbtools_rollback_probe`) })
	db.Exec(`DROP TABLE IF EXISTS dbtools_rollback_probe`)

	migration := `CREATE TABLE dbtools_rollback_probe (id INT PRIMARY KEY);
GO
THIS IS NOT SQL;`

	if err := (MSSQL{}).ExecMigration(ctx, conn, migration); err == nil {
		t.Fatal("ExecMigration with a bad second batch returned nil, want an error")
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sys.tables WHERE name = 'dbtools_rollback_probe'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("the first batch stayed committed after a later batch failed; the file is not atomic")
	}
}
