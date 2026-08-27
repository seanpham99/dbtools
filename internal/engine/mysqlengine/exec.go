package mysqlengine

import (
	"context"
	"database/sql"
)

// ExecMigration runs one migration file on conn. MySQL has no
// transactional DDL, so a file that fails partway leaves its earlier
// statements applied — which is precisely why the ledger marks the version
// "applying" before the file runs and why a surviving applying row blocks
// the next run.
//
// Multi-statement execution depends on MultiStatements being set on the
// DSN; dsnFromURL forces it rather than trusting the caller.
func (MySQL) ExecMigration(ctx context.Context, conn *sql.Conn, sqlText string) error {
	_, err := conn.ExecContext(ctx, sqlText)
	return err
}
