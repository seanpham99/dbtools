//go:build integration

package mysqlengine

import (
	"context"
	"os"
	"testing"
)

// MySQL executes only the first statement of a multi-statement Exec unless
// MultiStatements is set on the DSN — and reports success either way. That
// makes it the one engine where a wiring regression is silent: the ledger
// records a version as applied while most of its SQL never ran.
//
// dsnFromURL forces the flag, but a unit test on the DSN string only proves
// the string. This proves the path: two statements in, two effects out.
func TestIntegrationExecMigration_RunsEveryStatement(t *testing.T) {
	rawURL := os.Getenv("DBTOOLS_TEST_MYSQL_URL")
	if rawURL == "" {
		t.Skip("DBTOOLS_TEST_MYSQL_URL not set, skipping integration test")
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
		db.Exec(`DROP TABLE IF EXISTS dbtools_multi_a`)
		db.Exec(`DROP TABLE IF EXISTS dbtools_multi_b`)
	})
	db.Exec(`DROP TABLE IF EXISTS dbtools_multi_a`)
	db.Exec(`DROP TABLE IF EXISTS dbtools_multi_b`)

	migration := `CREATE TABLE dbtools_multi_a (id INT PRIMARY KEY);
CREATE TABLE dbtools_multi_b (id INT PRIMARY KEY);
INSERT INTO dbtools_multi_a (id) VALUES (1);`

	if err := (MySQL{}).ExecMigration(ctx, conn, migration); err != nil {
		t.Fatalf("ExecMigration: %v", err)
	}

	for _, table := range []string{"dbtools_multi_a", "dbtools_multi_b"} {
		var n int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM information_schema.tables
			 WHERE table_schema = DATABASE() AND table_name = ?`, table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("%s missing — only the first statement ran, which is the silent-truncation failure", table)
		}
	}
	var rows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM dbtools_multi_a`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("row count = %d, want 1 — the third statement did not run", rows)
	}
}
