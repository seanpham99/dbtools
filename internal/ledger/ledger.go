// Package ledger defines the types and interfaces for the migration audit
// ledger (the dbtools_migration_history table). Engine dialects (MSSQL,
// Postgres, SQLite) implement the LedgerStore interface in their respective
// engine packages.
package ledger

import (
	"database/sql"
	"fmt"
	"regexp"
	"time"
)

// Status is the tracked state of one migration version in the ledger.
type Status string

const (
	StatusApplied  Status = "applied"
	StatusReverted Status = "reverted"
)

// Entry is one row of dbtools_migration_history.
type Entry struct {
	Version    uint64
	Status     Status
	RecordedAt *time.Time // nil for backfilled rows whose original apply time is unknown
	Note       string
	// ContentSHA256 is the hex SHA-256 of the migration file that was
	// applied, or "" for backfilled rows recorded before hashes existed.
	ContentSHA256 string
}

// DBTX is the subset of *sql.DB's interface needed for ledger operations.
// Both *sql.DB and *sql.Tx satisfy it, so a caller that needs several
// writes to commit atomically (e.g. repair.Run) can pass a transaction
// instead of a raw connection.
type DBTX interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

var validTableName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidateTableName rejects a ledger table name that isn't a plain SQL
// identifier — table names are inlined into SQL text (they can't be bind
// parameters), so this is the injection guard.
func ValidateTableName(name string) error {
	if !validTableName.MatchString(name) {
		return fmt.Errorf("invalid ledger table name %q: must match %s", name, validTableName.String())
	}
	return nil
}

