// Package ledger defines the types and interfaces for the migration audit
// ledger (the dbtools_migration_history table). Engine dialects (MSSQL,
// Postgres, SQLite) implement the LedgerStore interface in their respective
// engine packages.
package ledger

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Status is the tracked state of one migration version in the ledger.
type Status string

const (
	StatusApplied  Status = "applied"
	StatusReverted Status = "reverted"
	// StatusApplying marks a migration whose SQL has started but not
	// finished. It is written before the file runs and replaced when the
	// file succeeds, so a row left in this state is the record of a run
	// that died partway through.
	//
	// This replaces golang-migrate's boolean "dirty" cursor flag and
	// carries strictly more information: dirty could only say that *a*
	// migration failed, while a surviving applying row names which one.
	StatusApplying Status = "applying"
)

// Statuses is every valid status, for the CHECK constraint each engine puts
// on its ledger table.
var Statuses = []Status{StatusApplied, StatusReverted, StatusApplying}

// StatusList renders Statuses as a quoted, comma-separated SQL list for
// inlining into a CHECK constraint, so the constraint and these constants
// cannot drift apart.
func StatusList() string {
	quoted := make([]string, len(Statuses))
	for i, s := range Statuses {
		quoted[i] = "'" + string(s) + "'"
	}
	return strings.Join(quoted, ", ")
}

// HashSourceAdopted marks an Entry's ContentSHA256 as recorded by `dbtools
// adopt` rather than observed at apply time — see Entry.HashSource.
const HashSourceAdopted = "adopted"

// Entry is one row of dbtools_migration_history.
type Entry struct {
	Version    uint64
	Status     Status
	RecordedAt *time.Time // nil for backfilled rows whose original apply time is unknown
	Note       string
	// ContentSHA256 is the hex SHA-256 of the migration file that was
	// applied, or "" for backfilled rows recorded before hashes existed.
	ContentSHA256 string
	// HashSource indicates the provenance of ContentSHA256: "" for normal applies,
	// "adopted" for rows imported without verification.
	HashSource string
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

// State is the ledger's answer to "where is this database?", replacing the
// (version, dirty) cursor golang-migrate kept in a separate table.
//
// Version is derived from the rows rather than stored, which is what makes
// the ledger a single source of truth: there is no second value that can
// disagree with the history it summarises.
type State struct {
	// Version is the highest applied version, meaningful only when
	// HasVersion is true.
	Version uint64
	// HasVersion is false for a database no migration has ever been
	// applied to. Distinguishing that from "version 0" matters, because a
	// squashed baseline is legitimately version 0.
	HasVersion bool
	// Applying names the migration that was mid-flight when a previous run
	// died, meaningful only when Dirty is true.
	Applying uint64
	// Dirty reports that a migration started and never finished. Applying
	// again would run SQL on a schema in an unknown state, so callers must
	// refuse and point at `dbtools repair`.
	Dirty bool
}

// QueryState derives the migration state from the ledger's own rows.
//
// The SQL is plain enough to be identical on every engine dbtools supports,
// so it lives here once rather than four times: engines delegate to it
// instead of each carrying a copy that could drift.
//
// table must already have passed ValidateTableName — it is inlined, because
// table names cannot be bind parameters.
func QueryState(db DBTX, table string) (State, error) {
	var applied, applying sql.NullInt64
	query := fmt.Sprintf(
		`SELECT (SELECT MAX(version) FROM %[1]s WHERE status = '%[2]s'),
		        (SELECT MIN(version) FROM %[1]s WHERE status = '%[3]s')`,
		table, StatusApplied, StatusApplying)
	if err := db.QueryRow(query).Scan(&applied, &applying); err != nil {
		return State{}, fmt.Errorf("reading migration state from %s: %w", table, err)
	}
	st := State{}
	if applied.Valid {
		st.Version = uint64(applied.Int64)
		st.HasVersion = true
	}
	if applying.Valid {
		st.Applying = uint64(applying.Int64)
		st.Dirty = true
	}
	return st, nil
}

// DirtyError describes a ledger left mid-apply, and is what every write
// path returns rather than proceeding. Applying more SQL on top of a schema
// in an unknown state is how a recoverable failure becomes an unrecoverable
// one.
type DirtyError struct {
	Version uint64
	Table   string
}

func (e *DirtyError) Error() string {
	return fmt.Sprintf(
		"migration %d started and never finished (its %s row is still %q), so the schema is in an unknown state. "+
			"Inspect that migration, then record the outcome with `dbtools repair <target> %d:applied` "+
			"or `dbtools repair <target> %d:reverted`",
		e.Version, e.Table, StatusApplying, e.Version, e.Version)
}
