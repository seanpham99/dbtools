package mssqlengine

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
)

// goBatchSeparator matches SQL Server's GO batch separator on its own line.
// GO is a client directive, not SQL: the server rejects it, so a migration
// file using it has to be split before execution.
var goBatchSeparator = regexp.MustCompile(`(?im)^\s*GO\s*$`)

// ExecMigration runs one migration file on conn, splitting it into batches
// on GO and running them inside a single transaction.
//
// SQL Server has transactional DDL, so wrapping the whole file means a
// failure partway leaves zero partial changes — the strongest guarantee of
// any engine dbtools supports, and worth keeping even though the ledger's
// applying row would catch it either way.
func (MSSQL) ExecMigration(ctx context.Context, conn *sql.Conn, sqlText string) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, batch := range goBatchSeparator.Split(sqlText, -1) {
		if strings.TrimSpace(batch) == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, batch); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
