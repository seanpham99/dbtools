package ledger

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/seanpham99/dbtools/internal/migrator"
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

// DBTX is the subset of *sql.DB's interface every function in this package
// needs. Both *sql.DB and *sql.Tx satisfy it, so a caller that needs several
// writes to commit atomically (e.g. repair.Run) can pass a transaction
// instead of a raw connection.
type DBTX interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// EnsureSchema creates dbtools_migration_history if it doesn't already
// exist, and adds the content_sha256 column to an existing table created
// by an earlier version of dbtools (idempotent either way).
func EnsureSchema(db DBTX) error {
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

// checkVersionRange rejects versions that would wrap when stored in the
// ledger's signed BIGINT column.
func checkVersionRange(version uint64) error {
	if version > 1<<63-1 {
		return fmt.Errorf("migration version %d exceeds the ledger's BIGINT range", version)
	}
	return nil
}

// Backfill inserts an "applied" row (recorded_at NULL, noted as backfilled)
// for every version in allVersions that is <= currentVersion and not
// already present in the ledger. If hasVersion is false, nothing is
// backfilled — no migration has ever been applied. Backfilled rows carry
// no content hash: the migration may have been applied before hashes
// existed.
func Backfill(db DBTX, currentVersion uint64, hasVersion bool, allVersions []uint64) error {
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
func SetStatus(db DBTX, version uint64, status Status, note string) error {
	if err := checkVersionRange(version); err != nil {
		return err
	}
	// HOLDLOCK serializes concurrent upserts of the same version so the
	// IF EXISTS / INSERT cannot race into a duplicate-PK error (repair can
	// run multiple writers in one transaction).
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

// SetStatusWithHash is SetStatus plus recording the applied migration
// file's content hash, so verify can detect edits after apply.
func SetStatusWithHash(db DBTX, version uint64, status Status, note, contentHash string) error {
	if err := checkVersionRange(version); err != nil {
		return err
	}
	// HOLDLOCK serializes concurrent upserts of the same version so the
	// IF EXISTS / INSERT cannot race into a duplicate-PK error (repair can
	// run multiple writers in one transaction).
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

// List returns every ledger row, ordered by version ascending.
func List(db DBTX) ([]Entry, error) {
	rows, err := db.Query(`SELECT version, status, recorded_at, note, content_sha256 FROM dbtools_migration_history ORDER BY version ASC`)
	if err != nil {
		return nil, fmt.Errorf("listing ledger: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		var e Entry
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
		e.Status = Status(status)
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
func AppliedVersions(db DBTX) ([]uint64, error) {
	entries, err := List(db)
	if err != nil {
		return nil, err
	}
	var versions []uint64
	for _, e := range entries {
		if e.Status == StatusApplied {
			versions = append(versions, e.Version)
		}
	}
	return versions, nil
}

// Sync ensures db's ledger table exists and has a backfilled row for every
// version m's cursor already considers applied — the ledger's baseline
// state before verify or repair reason about it.
//
// If the cursor is dirty (a previous apply failed partway), Sync refuses
// to backfill: the dirty version may not actually be applied, and writing
// "applied" for it would make the ledger lie about exactly the situation
// it exists to surface. Use `repair` to resolve the dirty cursor first.
func Sync(db *sql.DB, m *migrator.Migrator, migrationsDir string) error {
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
	allVersions, err := migrator.ListVersions(migrationsDir)
	if err != nil {
		return err
	}
	return Backfill(db, version, hasVersion, allVersions)
}
