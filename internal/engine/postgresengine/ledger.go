package postgresengine

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/seanpham99/dbtools/internal/ledger"
)

// ledgerStore is the Postgres dialect of the dbtools_migration_history
// ledger. It reuses the ledger package's Entry/Status/DBTX types and keeps
// exactly the semantics documented there — only the SQL differs.
type ledgerStore struct{}

func (ledgerStore) ensureSchema(db ledger.DBTX, table string) error {
	// Steady-state calls emit no server notices: the table and column adds
	// check the catalog first, so the IF NOT EXISTS forms below only fire
	// on a genuine cold start or a concurrent-ensure race. Routine
	// "already exists, skipping" NOTICEs on every command would either
	// flood the log or force a global notice suppression that would also
	// swallow a migration's own RAISE NOTICE saying the same thing.
	var tableExists bool
	if err := db.QueryRow(`SELECT to_regclass($1) IS NOT NULL`, table).Scan(&tableExists); err != nil {
		return fmt.Errorf("checking %s existence: %w", table, err)
	}
	if !tableExists {
		if _, err := db.Exec(fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %[1]s (
    version         BIGINT       NOT NULL PRIMARY KEY,
    status          VARCHAR(10)  NOT NULL CHECK (status IN (%[2]s)),
    recorded_at     TIMESTAMPTZ  NULL,
    note            VARCHAR(400) NULL,
    content_sha256  CHAR(64)     NULL,
    hash_source     VARCHAR(20)  NULL
)`, table, ledger.StatusList())); err != nil {
			return fmt.Errorf("ensuring %s schema: %w", table, err)
		}
	}
	for _, col := range []struct{ name, ddl string }{
		// Columns added by dbtools builds before content hashing / the
		// adopt command existed.
		{"content_sha256", fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS content_sha256 CHAR(64) NULL`, table)},
		{"hash_source", fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS hash_source VARCHAR(20) NULL`, table)},
	} {
		var hasCol bool
		if err := db.QueryRow(`
SELECT EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2
)`, table, col.name).Scan(&hasCol); err != nil {
			return fmt.Errorf("inspecting %s columns: %w", table, err)
		}
		if hasCol {
			continue
		}
		if _, err := db.Exec(col.ddl); err != nil {
			return fmt.Errorf("adding %s to %s: %w", col.name, table, err)
		}
	}
	return widenStatusConstraint(db, table)
}

// widenStatusConstraint replaces a pre-v0.7 two-value status CHECK with one
// covering every current status, so an upgraded database can record the
// "applying" state the runner writes before each migration.
//
// It inspects the constraint first and only rewrites when the current
// statuses are missing. EnsureSchema runs on nearly every command, and each
// ALTER TABLE takes an ACCESS EXCLUSIVE lock — doing that unconditionally
// would make routine status and doctor calls block each other, and rewrite
// the catalog every time, for no change at all.
func widenStatusConstraint(db ledger.DBTX, table string) error {
	var definition sql.NullString
	err := db.QueryRow(`
SELECT pg_get_constraintdef(c.oid)
FROM pg_constraint c
WHERE c.conrelid = $1::regclass AND c.contype = 'c' AND pg_get_constraintdef(c.oid) LIKE '%status%'
LIMIT 1`, table).Scan(&definition)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // no status constraint to widen
	}
	if err != nil {
		return fmt.Errorf("inspecting the status constraint on %s: %w", table, err)
	}
	if !definition.Valid || strings.Contains(definition.String, string(ledger.StatusApplying)) {
		return nil // already current
	}

	if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE %[1]s DROP CONSTRAINT IF EXISTS %[1]s_status_check`, table)); err != nil {
		return fmt.Errorf("dropping the old status constraint on %s: %w", table, err)
	}
	if _, err := db.Exec(fmt.Sprintf(
		`ALTER TABLE %[1]s ADD CONSTRAINT %[1]s_status_check CHECK (status IN (%[2]s))`,
		table, ledger.StatusList())); err != nil {
		return fmt.Errorf("widening the status constraint on %s: %w", table, err)
	}
	return nil
}

// checkVersionRange rejects versions that would wrap when stored in the
// ledger's signed BIGINT column.
func checkVersionRange(version uint64) error {
	if version > math.MaxInt64 {
		return fmt.Errorf("migration version %d exceeds the ledger's BIGINT range", version)
	}
	return nil
}

// SetStatus upserts version's ledger row, preserving content_sha256 on
// update (the applied file's hash must not be clobbered by a status edit).
func (ledgerStore) SetStatus(db ledger.DBTX, version uint64, status ledger.Status, note, table string) error {
	if err := checkVersionRange(version); err != nil {
		return err
	}
	_, err := db.Exec(fmt.Sprintf(`
INSERT INTO %s (version, status, recorded_at, note)
VALUES ($1, $2, now(), $3)
ON CONFLICT (version) DO UPDATE
SET status = EXCLUDED.status, recorded_at = EXCLUDED.recorded_at, note = EXCLUDED.note`, table),
		int64(version), string(status), note)
	if err != nil {
		return fmt.Errorf("setting status for version %d: %w", version, err)
	}
	return nil
}

// SetStatusWithHash is SetStatus plus recording the applied migration
// file's content hash, so verify can detect edits after apply.
func (ledgerStore) SetStatusWithHash(db ledger.DBTX, version uint64, status ledger.Status, note, contentHash, table string) error {
	if err := checkVersionRange(version); err != nil {
		return err
	}
	_, err := db.Exec(fmt.Sprintf(`
INSERT INTO %s (version, status, recorded_at, note, content_sha256)
VALUES ($1, $2, now(), $3, $4)
ON CONFLICT (version) DO UPDATE
SET status = EXCLUDED.status, recorded_at = EXCLUDED.recorded_at, note = EXCLUDED.note, content_sha256 = EXCLUDED.content_sha256, hash_source = ''`, table),
		int64(version), string(status), note, contentHash)
	if err != nil {
		return fmt.Errorf("setting status for version %d: %w", version, err)
	}
	return nil
}

// SetStatusAdopted records version as applied with hash_source "adopted".
func (ledgerStore) SetStatusAdopted(db ledger.DBTX, version uint64, note, contentHash, table string) error {
	if err := checkVersionRange(version); err != nil {
		return err
	}
	_, err := db.Exec(fmt.Sprintf(`
INSERT INTO %s (version, status, recorded_at, note, content_sha256, hash_source)
VALUES ($1, 'applied', now(), $2, $3, $4)
ON CONFLICT (version) DO UPDATE
SET status = EXCLUDED.status, recorded_at = EXCLUDED.recorded_at, note = EXCLUDED.note, content_sha256 = EXCLUDED.content_sha256, hash_source = EXCLUDED.hash_source`, table),
		int64(version), note, contentHash, ledger.HashSourceAdopted)
	if err != nil {
		return fmt.Errorf("setting adopted status for version %d: %w", version, err)
	}
	return nil
}

// List returns every ledger row, ordered by version ascending.
func (ledgerStore) List(db ledger.DBTX, table string) ([]ledger.Entry, error) {
	rows, err := db.Query(fmt.Sprintf(`SELECT version, status, recorded_at, note, content_sha256, hash_source FROM %s ORDER BY version ASC`, table))
	if err != nil {
		return nil, fmt.Errorf("listing ledger: %w", err)
	}
	defer rows.Close()

	var entries []ledger.Entry
	for rows.Next() {
		var e ledger.Entry
		var version int64
		var status string
		var recordedAt sql.NullTime
		var note, contentHash, hashSource sql.NullString
		if err := rows.Scan(&version, &status, &recordedAt, &note, &contentHash, &hashSource); err != nil {
			return nil, fmt.Errorf("scanning ledger row: %w", err)
		}
		if version < 0 {
			return nil, fmt.Errorf("ledger contains negative version %d; the table was modified outside dbtools", version)
		}
		e.Version = uint64(version)
		e.Status = ledger.Status(status)
		if recordedAt.Valid {
			t := recordedAt.Time
			e.RecordedAt = &t
		}
		e.Note = note.String
		e.ContentSHA256 = contentHash.String
		e.HashSource = hashSource.String
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// AppliedVersions returns every version currently marked "applied", ascending.
func (s ledgerStore) AppliedVersions(db ledger.DBTX, table string) ([]uint64, error) {
	entries, err := s.List(db, table)
	if err != nil {
		return nil, err
	}
	var versions []uint64
	for _, e := range entries {
		if e.Status == ledger.StatusApplied {
			versions = append(versions, e.Version)
		}
	}
	return versions, nil
}

// Sync mirrors ledger.Sync for Postgres: ensure the table exists and
// backfill a row for every version m's cursor already considers applied.
// Refuses to backfill when the cursor is dirty (a previous apply failed
// partway) — see ledger.Sync.
func (s ledgerStore) EnsureSchema(db ledger.DBTX, table string) error {
	return s.ensureSchema(db, table)
}

// State derives the migration state from the ledger's own rows. The SQL is
// identical on every engine, so it lives in the ledger package rather than
// as four copies that could drift.
func (ledgerStore) State(db ledger.DBTX, table string) (ledger.State, error) {
	return ledger.QueryState(db, table)
}
