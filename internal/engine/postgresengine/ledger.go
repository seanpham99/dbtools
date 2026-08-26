package postgresengine

import (
	"database/sql"
	"fmt"
	"math"

	"github.com/seanpham99/dbtools/internal/ledger"
	"github.com/seanpham99/dbtools/internal/migrator"
)

// ledgerStore is the Postgres dialect of the dbtools_migration_history
// ledger. It reuses the ledger package's Entry/Status/DBTX types and keeps
// exactly the semantics documented there — only the SQL differs.
type ledgerStore struct{}

func (ledgerStore) ensureSchema(db ledger.DBTX, table string) error {
	_, err := db.Exec(fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %s (
    version         BIGINT       NOT NULL PRIMARY KEY,
    status          VARCHAR(10)  NOT NULL CHECK (status IN ('applied', 'reverted')),
    recorded_at     TIMESTAMPTZ  NULL,
    note            VARCHAR(400) NULL,
    content_sha256  CHAR(64)     NULL,
    hash_source     VARCHAR(20)  NULL
)`, table))
	if err != nil {
		return fmt.Errorf("ensuring %s schema: %w", table, err)
	}
	// Column added by dbtools builds before content hashing existed —
	// idempotent when already present.
	_, err = db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS content_sha256 CHAR(64) NULL`, table))
	if err != nil {
		return fmt.Errorf("adding content_sha256 to %s: %w", table, err)
	}
	// Column added for adopt command.
	_, err = db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN IF NOT EXISTS hash_source VARCHAR(20) NULL`, table))
	if err != nil {
		return fmt.Errorf("adding hash_source to %s: %w", table, err)
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

func (ledgerStore) backfill(db ledger.DBTX, currentVersion uint64, hasVersion bool, allVersions []uint64, table string) error {
	if !hasVersion {
		return nil
	}
	for _, v := range allVersions {
		if v > currentVersion {
			continue
		}
		if err := checkVersionRange(v); err != nil {
			return err
		}
		_, err := db.Exec(fmt.Sprintf(`
INSERT INTO %s (version, status, recorded_at, note)
VALUES ($1, 'applied', NULL, 'backfilled: applied before ledger existed')
ON CONFLICT (version) DO NOTHING`, table), int64(v))
		if err != nil {
			return fmt.Errorf("backfilling version %d: %w", v, err)
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
SET status = EXCLUDED.status, recorded_at = EXCLUDED.recorded_at, note = EXCLUDED.note`, table),
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
VALUES ($1, 'applied', now(), $2, $3, 'adopted')
ON CONFLICT (version) DO UPDATE
SET status = EXCLUDED.status, recorded_at = EXCLUDED.recorded_at, note = EXCLUDED.note, content_sha256 = EXCLUDED.content_sha256, hash_source = EXCLUDED.hash_source`, table),
		int64(version), note, contentHash)
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
func (s ledgerStore) Sync(db *sql.DB, m *migrator.Migrator, migrationsDir, upSuffix, table string) error {
	if err := s.ensureSchema(db, table); err != nil {
		return err
	}
	version, dirty, hasVersion, err := m.Version()
	if err != nil {
		return err
	}
	if dirty {
		return fmt.Errorf("migration cursor is dirty (a previous apply failed partway through version %d); run `dbtools repair <target>` to resolve it before syncing the ledger", version)
	}
	allVersions, err := migrator.ListVersions(migrationsDir, upSuffix)
	if err != nil {
		return err
	}
	return s.backfill(db, version, hasVersion, allVersions, table)
}


