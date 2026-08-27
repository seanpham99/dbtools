package sqliteengine

import (
	"context"
	"database/sql"
)

// ExecMigration runs one migration file on conn inside a single
// transaction.
//
// The transaction is explicit because a multi-statement Exec is not one:
// SQLite runs each statement in its own implicit transaction, so a file
// whose third statement fails would leave the first two committed. SQLite
// does have transactional DDL, which is what makes wrapping the whole file
// possible — and worth doing, since it is the strongest recovery guarantee
// any engine here offers.
func (SQLite) ExecMigration(ctx context.Context, conn *sql.Conn, sqlText string) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, sqlText); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
