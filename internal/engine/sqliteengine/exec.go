package sqliteengine

import (
	"context"
	"database/sql"
)

// ExecMigration runs one migration file on conn. modernc.org/sqlite
// executes a multi-statement string directly, and SQLite's DDL is
// transactional, so a failing file rolls back on its own.
func (SQLite) ExecMigration(ctx context.Context, conn *sql.Conn, sqlText string) error {
	_, err := conn.ExecContext(ctx, sqlText)
	return err
}
