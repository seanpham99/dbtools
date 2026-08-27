package postgresengine

import (
	"context"
	"database/sql"
	"fmt"
)

// ExecMigration runs one migration file on conn.
//
// The session is reset first. A migration run reuses one connection for
// every file, so any file that changes session state — and every
// pg_dump-generated baseline does, its header being
// set_config('search_path', ”, false) — would otherwise poison every later
// migration in the same run: unqualified names resolve against the wrong
// schema, and RAISE NOTICE output disappears for the rest of the session.
//
// Resetting per file is cheaper than a fresh connection per file and makes
// each file behave as though it ran in a clean session.
func (Postgres) ExecMigration(ctx context.Context, conn *sql.Conn, sqlText string) error {
	if _, err := conn.ExecContext(ctx, "SET search_path TO public; RESET client_min_messages;"); err != nil {
		return fmt.Errorf("resetting session state before the migration: %w", err)
	}
	// lib/pq sends a parameterless Exec over the simple query protocol, so
	// a multi-statement file runs as one implicit transaction.
	if _, err := conn.ExecContext(ctx, sqlText); err != nil {
		return err
	}
	return nil
}
