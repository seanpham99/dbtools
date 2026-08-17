package postgresengine

import (
	"database/sql"
	"fmt"
	"math"

	"github.com/dbtools/dbtools/internal/ledger"
	"github.com/dbtools/dbtools/internal/migrator"
)

// ledgerStore is the Postgres dialect of the dbtools_migration_history
// ledger. It reuses the ledger package's Entry/Status/DBTX types and keeps
// exactly the semantics documented there — only the SQL differs.
type ledgerStore struct{}

func (ledgerStore) ensureSchema(db ledger.DBTX) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS dbtools_migration_history (
    version     BIGINT       NOT NULL PRIMARY KEY,
    status      VARCHAR(10)  NOT NULL CHECK (status IN ('applied', 'reverted')),
    recorded_at TIMESTAMPTZ  NULL,
    note        VARCHAR(400) NULL
)`)
	if err != nil {
		return fmt.Errorf("ensuring dbtools_migration_history schema: %w", err)
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

func (ledgerStore) backfill(db ledger.DBTX, currentVersion uint64, hasVersion bool, allVersions []uint64) error {
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
		_, err := db.Exec(`
INSERT INTO dbtools_migration_history (version, status, recorded_at, note)
VALUES ($1, 'applied', NULL, 'backfilled: applied before ledger existed')
ON CONFLICT (version) DO NOTHING`, int64(v))
		if err != nil {
			return fmt.Errorf("backfilling version %d: %w", v, err)
		}
	}
	return nil
}

// SetStatus upserts version's ledger row.
func (ledgerStore) SetStatus(db ledger.DBTX, version uint64, status ledger.Status, note string) error {
	if err := checkVersionRange(version); err != nil {
		return err
	}
	_, err := db.Exec(`
INSERT INTO dbtools_migration_history (version, status, recorded_at, note)
VALUES ($1, $2, now(), $3)
ON CONFLICT (version) DO UPDATE
SET status = EXCLUDED.status, recorded_at = EXCLUDED.recorded_at, note = EXCLUDED.note`,
		int64(version), string(status), note)
	if err != nil {
		return fmt.Errorf("setting status for version %d: %w", version, err)
	}
	return nil
}

// List returns every ledger row, ordered by version ascending.
func (ledgerStore) List(db ledger.DBTX) ([]ledger.Entry, error) {
	rows, err := db.Query(`SELECT version, status, recorded_at, note FROM dbtools_migration_history ORDER BY version ASC`)
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
		var note sql.NullString
		if err := rows.Scan(&version, &status, &recordedAt, &note); err != nil {
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
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// AppliedVersions returns every version currently marked "applied", ascending.
func (s ledgerStore) AppliedVersions(db ledger.DBTX) ([]uint64, error) {
	entries, err := s.List(db)
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
func (s ledgerStore) Sync(db *sql.DB, m *migrator.Migrator, migrationsDir string) error {
	if err := s.ensureSchema(db); err != nil {
		return err
	}
	version, _, hasVersion, err := m.Version()
	if err != nil {
		return err
	}
	allVersions, err := migrator.ListVersions(migrationsDir)
	if err != nil {
		return err
	}
	return s.backfill(db, version, hasVersion, allVersions)
}
