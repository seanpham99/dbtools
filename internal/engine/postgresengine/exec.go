package postgresengine

import (
	"context"
	"database/sql"
	"fmt"
)

// sessionReset is prepended to every migration. A run reuses one connection
// for every file, so any file that changes session state — and every
// pg_dump-generated baseline does, its header being
// set_config('search_path', ”, false) — would otherwise poison every later
// migration in the same run: unqualified names resolve against the wrong
// schema, and RAISE NOTICE output disappears for the rest of the session.
//
// It is sent in the same Exec rather than a separate round trip so that the
// character offset Postgres reports on failure can be translated back to a
// line in the migration file, discounting this prefix.
const sessionReset = "SET search_path TO public; RESET client_min_messages;\n"

// ExecMigration runs one migration file on conn.
//
// Failures go through DiagnosePostgresError, which turns a bare driver
// error into something actionable: the failing line and column of the
// migration file, and — for SQLSTATE 42501 — which role the connection is
// authenticated as and what it is missing. That matters most where dbtools
// is hardest to debug, which is a private-network job whose only output is
// its log.
//
// The diagnostic runs on conn, not a pooled handle: "which role am I?" has
// a different answer on a different session, which would make the report
// confidently wrong.
func (Postgres) ExecMigration(ctx context.Context, conn *sql.Conn, sqlText string) error {
	// lib/pq sends a parameterless Exec over the simple query protocol, so
	// a multi-statement file runs as one implicit transaction.
	if _, err := conn.ExecContext(ctx, sessionReset+sqlText); err != nil {
		return fmt.Errorf("running migration: %w",
			DiagnosePostgresError(ctx, conn, err, sqlText, len([]rune(sessionReset))))
	}
	return nil
}
