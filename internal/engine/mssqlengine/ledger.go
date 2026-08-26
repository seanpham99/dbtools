package mssqlengine

import (
	"database/sql"
	"fmt"

	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/ledger"
	"github.com/seanpham99/dbtools/internal/migrator"
)

// EnsureSchema creates dbtools_migration_history if it doesn't already exist.
func EnsureSchema(db ledger.DBTX) error {
	_, err := db.Exec(`
IF OBJECT_ID(N'dbtools_migration_history', N'U') IS NULL
BEGIN
    CREATE TABLE dbtools_migration_history (
        version         BIGINT        NOT NULL PRIMARY KEY,
        status          VARCHAR(10)   NOT NULL CHECK (status IN ('applied', 'reverted')),
        recorded_at     DATETIME2(0)  NULL,
        note            NVARCHAR(400) NULL,
        content_sha256  CHAR(64)      NULL
    );
END;
ELSE IF COL_LENGTH(N'dbtools_migration_history', N'content_sha256') IS NULL
BEGIN
    ALTER TABLE dbtools_migration_history ADD content_sha256 CHAR(64) NULL;
END;`)
	if err != nil {
		return fmt.Errorf("ensuring dbtools_migration_history schema: %w", err)
	}
	return nil
}

func checkVersionRange(version uint64) error {
	if version > 1<<63-1 {
		return fmt.Errorf("migration version %d exceeds the ledger's BIGINT range", version)
	}
	return nil
}

// Backfill inserts an "applied" row for every version <= currentVersion.
func Backfill(db ledger.DBTX, currentVersion uint64, hasVersion bool, allVersions []uint64) error {
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
IF NOT EXISTS (SELECT 1 FROM dbtools_migration_history WHERE version = @p1)
INSERT INTO dbtools_migration_history (version, status, recorded_at, note)
VALUES (@p1, 'applied', NULL, 'backfilled: applied before ledger existed');`, int64(v))
		if err != nil {
			return fmt.Errorf("backfilling version %d: %w", v, err)
		}
	}
	return nil
}

// SetStatus upserts version's ledger row.
func SetStatus(db ledger.DBTX, version uint64, status ledger.Status, note string) error {
	if err := checkVersionRange(version); err != nil {
		return err
	}
	_, err := db.Exec(`
IF EXISTS (SELECT 1 FROM dbtools_migration_history WITH (HOLDLOCK) WHERE version = @p1)
    UPDATE dbtools_migration_history
    SET status = @p2, recorded_at = SYSUTCDATETIME(), note = @p3
    WHERE version = @p1
ELSE
    INSERT INTO dbtools_migration_history (version, status, recorded_at, note)
    VALUES (@p1, @p2, SYSUTCDATETIME(), @p3);`, int64(version), string(status), note)
	if err != nil {
		return fmt.Errorf("setting status for version %d: %w", version, err)
	}
	return nil
}

// SetStatusWithHash is SetStatus plus recording the migration file's content hash.
func SetStatusWithHash(db ledger.DBTX, version uint64, status ledger.Status, note, contentHash string) error {
	if err := checkVersionRange(version); err != nil {
		return err
	}
	_, err := db.Exec(`
IF EXISTS (SELECT 1 FROM dbtools_migration_history WITH (HOLDLOCK) WHERE version = @p1)
    UPDATE dbtools_migration_history
    SET status = @p2, recorded_at = SYSUTCDATETIME(), note = @p3
    WHERE version = @p1
ELSE
    INSERT INTO dbtools_migration_history (version, status, recorded_at, note, content_sha256)
    VALUES (@p1, @p2, SYSUTCDATETIME(), @p3, @p4);`, int64(version), string(status), note, contentHash)
	if err != nil {
		return fmt.Errorf("setting status for version %d: %w", version, err)
	}
	return nil
}

// List returns every ledger row in MSSQL, ordered by version ascending.
func List(db ledger.DBTX) ([]ledger.Entry, error) {
	rows, err := db.Query(`SELECT version, status, recorded_at, note, content_sha256 FROM dbtools_migration_history ORDER BY version ASC`)
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
		var note, contentHash sql.NullString
		if err := rows.Scan(&version, &status, &recordedAt, &note, &contentHash); err != nil {
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
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// AppliedVersions returns every version currently marked "applied", ascending.
func AppliedVersions(db ledger.DBTX) ([]uint64, error) {
	entries, err := List(db)
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

// Sync ensures MSSQL db's ledger table exists and is backfilled.
func Sync(db *sql.DB, m *migrator.Migrator, migrationsDir, upSuffix string) error {
	if err := EnsureSchema(db); err != nil {
		return err
	}
	version, dirty, hasVersion, err := m.Version()
	if err != nil {
		return err
	}
	if dirty {
		return fmt.Errorf("migration cursor is dirty (a previous apply failed partway through version %d); run `dbtools repair %s` to resolve it before syncing the ledger", version, "target")
	}
	allVersions, err := migrator.ListVersions(migrationsDir, upSuffix)
	if err != nil {
		return err
	}
	return Backfill(db, version, hasVersion, allVersions)
}

type mssqlLedgerStore struct{}

func (mssqlLedgerStore) Sync(db *sql.DB, m *migrator.Migrator, migrationsDir, upSuffix string) error {
	return Sync(db, m, migrationsDir, upSuffix)
}

func (mssqlLedgerStore) SetStatus(db ledger.DBTX, version uint64, status ledger.Status, note string) error {
	return SetStatus(db, version, status, note)
}

func (mssqlLedgerStore) SetStatusWithHash(db ledger.DBTX, version uint64, status ledger.Status, note, contentHash string) error {
	return SetStatusWithHash(db, version, status, note, contentHash)
}

func (mssqlLedgerStore) List(db ledger.DBTX) ([]ledger.Entry, error) {
	return List(db)
}

func (mssqlLedgerStore) AppliedVersions(db ledger.DBTX) ([]uint64, error) {
	return AppliedVersions(db)
}

var _ engine.LedgerStore = mssqlLedgerStore{}
