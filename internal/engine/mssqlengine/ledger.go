package mssqlengine

import (
	"database/sql"
	"fmt"

	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/ledger"
)

// EnsureSchema creates table if it doesn't already exist.
func EnsureSchema(db ledger.DBTX, table string) error {
	_, err := db.Exec(fmt.Sprintf(`
IF OBJECT_ID(N'%[1]s', N'U') IS NULL
BEGIN
    CREATE TABLE %[1]s (
        version         BIGINT        NOT NULL PRIMARY KEY,
        status          VARCHAR(10)   NOT NULL CHECK (status IN (%[2]s)),
        recorded_at     DATETIME2(0)  NULL,
        note            NVARCHAR(400) NULL,
        content_sha256  CHAR(64)      NULL,
        hash_source     VARCHAR(20)   NULL
    );
END;
ELSE
BEGIN
    IF COL_LENGTH(N'%[1]s', N'content_sha256') IS NULL
    BEGIN
        ALTER TABLE %[1]s ADD content_sha256 CHAR(64) NULL;
    END;
    IF COL_LENGTH(N'%[1]s', N'hash_source') IS NULL
    BEGIN
        ALTER TABLE %[1]s ADD hash_source VARCHAR(20) NULL;
    END;
END;`, table, ledger.StatusList()))
	if err != nil {
		return fmt.Errorf("ensuring %s schema: %w", table, err)
	}
	return nil
}

func checkVersionRange(version uint64) error {
	if version > 1<<63-1 {
		return fmt.Errorf("migration version %d exceeds the ledger's BIGINT range", version)
	}
	return nil
}

// SetStatus upserts version's ledger row.
func SetStatus(db ledger.DBTX, version uint64, status ledger.Status, note, table string) error {
	if err := checkVersionRange(version); err != nil {
		return err
	}
	_, err := db.Exec(fmt.Sprintf(`
IF EXISTS (SELECT 1 FROM %s WITH (HOLDLOCK) WHERE version = @p1)
    UPDATE %s
    SET status = @p2, recorded_at = SYSUTCDATETIME(), note = @p3
    WHERE version = @p1
ELSE
    INSERT INTO %s (version, status, recorded_at, note)
    VALUES (@p1, @p2, SYSUTCDATETIME(), @p3);`, table, table, table), int64(version), string(status), note)
	if err != nil {
		return fmt.Errorf("setting status for version %d: %w", version, err)
	}
	return nil
}

// SetStatusWithHash is SetStatus plus recording the migration file's content hash.
func SetStatusWithHash(db ledger.DBTX, version uint64, status ledger.Status, note, contentHash, table string) error {
	if err := checkVersionRange(version); err != nil {
		return err
	}
	_, err := db.Exec(fmt.Sprintf(`
IF EXISTS (SELECT 1 FROM %s WITH (HOLDLOCK) WHERE version = @p1)
    UPDATE %s
    SET status = @p2, recorded_at = SYSUTCDATETIME(), note = @p3, content_sha256 = @p4, hash_source = ''
    WHERE version = @p1
ELSE
    INSERT INTO %s (version, status, recorded_at, note, content_sha256)
    VALUES (@p1, @p2, SYSUTCDATETIME(), @p3, @p4);`, table, table, table), int64(version), string(status), note, contentHash)
	if err != nil {
		return fmt.Errorf("setting status for version %d: %w", version, err)
	}
	return nil
}

// SetStatusAdopted records version as applied with hash_source "adopted".
func SetStatusAdopted(db ledger.DBTX, version uint64, note, contentHash, table string) error {
	if err := checkVersionRange(version); err != nil {
		return err
	}
	_, err := db.Exec(fmt.Sprintf(`
IF EXISTS (SELECT 1 FROM %s WITH (HOLDLOCK) WHERE version = @p1)
    UPDATE %s
    SET status = 'applied', recorded_at = SYSUTCDATETIME(), note = @p2, content_sha256 = @p3, hash_source = @p4
    WHERE version = @p1
ELSE
    INSERT INTO %s (version, status, recorded_at, note, content_sha256, hash_source)
    VALUES (@p1, 'applied', SYSUTCDATETIME(), @p2, @p3, @p4);`, table, table, table), int64(version), note, contentHash, ledger.HashSourceAdopted)
	if err != nil {
		return fmt.Errorf("setting adopted status for version %d: %w", version, err)
	}
	return nil
}

// List returns every ledger row in MSSQL, ordered by version ascending.
func List(db ledger.DBTX, table string) ([]ledger.Entry, error) {
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
func AppliedVersions(db ledger.DBTX, table string) ([]uint64, error) {
	entries, err := List(db, table)
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

type mssqlLedgerStore struct{}

func (mssqlLedgerStore) EnsureSchema(db ledger.DBTX, table string) error {
	return EnsureSchema(db, table)
}

func (mssqlLedgerStore) SetStatus(db ledger.DBTX, version uint64, status ledger.Status, note, table string) error {
	return SetStatus(db, version, status, note, table)
}

func (mssqlLedgerStore) SetStatusWithHash(db ledger.DBTX, version uint64, status ledger.Status, note, contentHash, table string) error {
	return SetStatusWithHash(db, version, status, note, contentHash, table)
}

func (mssqlLedgerStore) SetStatusAdopted(db ledger.DBTX, version uint64, note, contentHash, table string) error {
	return SetStatusAdopted(db, version, note, contentHash, table)
}

func (mssqlLedgerStore) List(db ledger.DBTX, table string) ([]ledger.Entry, error) {
	return List(db, table)
}

func (mssqlLedgerStore) AppliedVersions(db ledger.DBTX, table string) ([]uint64, error) {
	return AppliedVersions(db, table)
}

var _ engine.LedgerStore = mssqlLedgerStore{}

// State derives the migration state from the ledger's own rows. The SQL is
// identical on every engine, so it lives in the ledger package rather than
// as four copies that could drift.
func (mssqlLedgerStore) State(db ledger.DBTX, table string) (ledger.State, error) {
	return ledger.QueryState(db, table)
}
