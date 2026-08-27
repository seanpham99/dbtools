package sqliteengine

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/seanpham99/dbtools/internal/ledger"
)

// ledgerStore is the SQLite dialect of the dbtools_migration_history
// ledger. Semantics match the ledger package exactly — only the SQL
// differs. recorded_at is written as a Go time.Time parameter (stored in
// the driver's canonical text form) so it scans back losslessly.
type ledgerStore struct{}

// checkVersionRange rejects versions that would wrap when stored in the
// ledger's signed INTEGER column.
func checkVersionRange(version uint64) error {
	if version > math.MaxInt64 {
		return fmt.Errorf("migration version %d exceeds the ledger's INTEGER range", version)
	}
	return nil
}

func (ledgerStore) ensureSchema(db ledger.DBTX, table string) error {
	_, err := db.Exec(fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %[1]s (
    version         INTEGER NOT NULL PRIMARY KEY,
    status          TEXT    NOT NULL CHECK (status IN (%[2]s)),
    recorded_at     TIMESTAMP NULL,
    note            TEXT    NULL,
    content_sha256  TEXT    NULL,
    hash_source     TEXT    NULL
)`, table, ledger.StatusList()))
	if err != nil {
		return fmt.Errorf("ensuring %s schema: %w", table, err)
	}
	// Column added by dbtools builds before content hashing existed.
	cols, err := db.Query(fmt.Sprintf(`SELECT name FROM pragma_table_info('%s') WHERE name = 'content_sha256'`, table))
	if err != nil {
		return fmt.Errorf("inspecting %s columns: %w", table, err)
	}
	hasContentHash := cols.Next()
	// Closed immediately rather than deferred: SQLite refuses schema
	// changes while a read cursor is open on the same connection, and the
	// constraint widen below rebuilds this table.
	cols.Close()
	if !hasContentHash {
		if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN content_sha256 TEXT NULL`, table)); err != nil {
			return fmt.Errorf("adding content_sha256 to %s: %w", table, err)
		}
	}
	// Column added for adopt command.
	srcCols, err := db.Query(fmt.Sprintf(`SELECT name FROM pragma_table_info('%s') WHERE name = 'hash_source'`, table))
	if err != nil {
		return fmt.Errorf("inspecting %s columns: %w", table, err)
	}
	hasHashSource := srcCols.Next()
	srcCols.Close()
	if !hasHashSource {
		if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN hash_source TEXT NULL`, table)); err != nil {
			return fmt.Errorf("adding hash_source to %s: %w", table, err)
		}
	}
	return widenStatusConstraint(db, table)
}

// widenStatusConstraint replaces a pre-v0.7 two-value status CHECK with one
// covering every current status.
//
// A ledger created before v0.7 rejects "applying", so the first migration
// run against an upgraded database would fail on its own bookkeeping.
// SQLite cannot ALTER a CHECK constraint, so the table is rebuilt — the
// documented approach, and cheap here because a ledger holds one row per
// migration.
//
// Detection is on the stored DDL rather than a trial insert: a failed
// insert would have to be rolled back, and doing that inside a caller's
// transaction is not something this function can assume it may do.
func widenStatusConstraint(db ledger.DBTX, table string) error {
	var ddl string
	err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&ddl)
	if err != nil {
		return fmt.Errorf("reading %s definition: %w", table, err)
	}
	if !strings.Contains(ddl, "CHECK (status IN") || strings.Contains(ddl, string(ledger.StatusApplying)) {
		return nil // no constraint to widen, or already current
	}

	tmp := table + "_v07_migration"
	stmts := []string{
		fmt.Sprintf(`DROP TABLE IF EXISTS %s`, tmp),
		fmt.Sprintf(`CREATE TABLE %[1]s (
    version         INTEGER NOT NULL PRIMARY KEY,
    status          TEXT    NOT NULL CHECK (status IN (%[2]s)),
    recorded_at     TIMESTAMP NULL,
    note            TEXT    NULL,
    content_sha256  TEXT    NULL,
    hash_source     TEXT    NULL
)`, tmp, ledger.StatusList()),
		fmt.Sprintf(`INSERT INTO %[1]s (version, status, recorded_at, note, content_sha256, hash_source)
SELECT version, status, recorded_at, note, content_sha256, hash_source FROM %[2]s`, tmp, table),
		fmt.Sprintf(`DROP TABLE %s`, table),
		fmt.Sprintf(`ALTER TABLE %s RENAME TO %s`, tmp, table),
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("widening the status constraint on %s: %w", table, err)
		}
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
VALUES (?, ?, ?, ?)
ON CONFLICT (version) DO UPDATE
SET status = excluded.status, recorded_at = excluded.recorded_at, note = excluded.note`, table),
		int64(version), string(status), time.Now().UTC(), note)
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
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (version) DO UPDATE
SET status = excluded.status, recorded_at = excluded.recorded_at, note = excluded.note, content_sha256 = excluded.content_sha256, hash_source = ''`, table),
		int64(version), string(status), time.Now().UTC(), note, contentHash)
	if err != nil {
		return fmt.Errorf("setting status for version %d: %w", version, err)
	}
	return nil
}

// SetStatusAdopted records version as applied with hash_source
// ledger.HashSourceAdopted.
func (ledgerStore) SetStatusAdopted(db ledger.DBTX, version uint64, note, contentHash, table string) error {
	if err := checkVersionRange(version); err != nil {
		return err
	}
	_, err := db.Exec(fmt.Sprintf(`
INSERT INTO %s (version, status, recorded_at, note, content_sha256, hash_source)
VALUES (?, 'applied', ?, ?, ?, ?)
ON CONFLICT (version) DO UPDATE
SET status = excluded.status, recorded_at = excluded.recorded_at, note = excluded.note, content_sha256 = excluded.content_sha256, hash_source = excluded.hash_source`, table),
		int64(version), time.Now().UTC(), note, contentHash, ledger.HashSourceAdopted)
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

// Sync mirrors ledger.Sync for SQLite: ensure the table exists and
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
