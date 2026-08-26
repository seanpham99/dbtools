package mysqlengine

import (
	"database/sql"
	"fmt"
	"math"

	"github.com/seanpham99/dbtools/internal/ledger"
	"github.com/seanpham99/dbtools/internal/migrator"
)

// mysqlLedgerStore is the MySQL dialect of the dbtools_migration_history
// ledger. Semantics match internal/ledger exactly — only the SQL differs.
type mysqlLedgerStore struct{}

func (mysqlLedgerStore) ensureSchema(db ledger.DBTX) error {
	_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS dbtools_migration_history (
    version         BIGINT       NOT NULL PRIMARY KEY,
    status          VARCHAR(10)  NOT NULL,
    recorded_at     DATETIME     NULL,
    note            VARCHAR(400) NULL,
    content_sha256  CHAR(64)     NULL,
    CHECK (status IN ('applied', 'reverted'))
) ENGINE=InnoDB`)
	if err != nil {
		return fmt.Errorf("ensuring dbtools_migration_history schema: %w", err)
	}
	// Column added by dbtools builds before content hashing existed.
	// MySQL's "ADD COLUMN IF NOT EXISTS" is version-gated (8.0.29+), so
	// check information_schema first for broader compatibility — same
	// approach as sqliteengine's pragma_table_info check.
	cols, err := db.Query(`
SELECT COLUMN_NAME FROM information_schema.columns
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'dbtools_migration_history' AND COLUMN_NAME = 'content_sha256'`)
	if err != nil {
		return fmt.Errorf("inspecting dbtools_migration_history columns: %w", err)
	}
	defer cols.Close()
	if !cols.Next() {
		if _, err := db.Exec(`ALTER TABLE dbtools_migration_history ADD COLUMN content_sha256 CHAR(64) NULL`); err != nil {
			return fmt.Errorf("adding content_sha256 to dbtools_migration_history: %w", err)
		}
	}
	return nil
}

func checkVersionRange(version uint64) error {
	if version > math.MaxInt64 {
		return fmt.Errorf("migration version %d exceeds the ledger's BIGINT range", version)
	}
	return nil
}

func (mysqlLedgerStore) backfill(db ledger.DBTX, currentVersion uint64, hasVersion bool, allVersions []uint64) error {
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
INSERT IGNORE INTO dbtools_migration_history (version, status, recorded_at, note)
VALUES (?, 'applied', NULL, 'backfilled: applied before ledger existed')`, int64(v))
		if err != nil {
			return fmt.Errorf("backfilling version %d: %w", v, err)
		}
	}
	return nil
}

// SetStatus upserts version's ledger row, preserving content_sha256 on
// update — the ON DUPLICATE KEY UPDATE clause deliberately omits it.
func (mysqlLedgerStore) SetStatus(db ledger.DBTX, version uint64, status ledger.Status, note string) error {
	if err := checkVersionRange(version); err != nil {
		return err
	}
	_, err := db.Exec(`
INSERT INTO dbtools_migration_history (version, status, recorded_at, note)
VALUES (?, ?, NOW(), ?)
ON DUPLICATE KEY UPDATE status = VALUES(status), recorded_at = VALUES(recorded_at), note = VALUES(note)`,
		int64(version), string(status), note)
	if err != nil {
		return fmt.Errorf("setting status for version %d: %w", version, err)
	}
	return nil
}

// SetStatusWithHash is SetStatus plus recording the applied migration
// file's content hash, so verify can detect edits after apply.
func (mysqlLedgerStore) SetStatusWithHash(db ledger.DBTX, version uint64, status ledger.Status, note, contentHash string) error {
	if err := checkVersionRange(version); err != nil {
		return err
	}
	_, err := db.Exec(`
INSERT INTO dbtools_migration_history (version, status, recorded_at, note, content_sha256)
VALUES (?, ?, NOW(), ?, ?)
ON DUPLICATE KEY UPDATE status = VALUES(status), recorded_at = VALUES(recorded_at), note = VALUES(note), content_sha256 = VALUES(content_sha256)`,
		int64(version), string(status), note, contentHash)
	if err != nil {
		return fmt.Errorf("setting status for version %d: %w", version, err)
	}
	return nil
}

// List returns every ledger row, ordered by version ascending.
func (mysqlLedgerStore) List(db ledger.DBTX) ([]ledger.Entry, error) {
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
func (s mysqlLedgerStore) AppliedVersions(db ledger.DBTX) ([]uint64, error) {
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

// Sync ensures MySQL db's ledger table exists and is backfilled. Refuses
// to backfill when the cursor is dirty (a previous apply failed partway).
func (s mysqlLedgerStore) Sync(db *sql.DB, m *migrator.Migrator, migrationsDir, upSuffix string) error {
	if err := s.ensureSchema(db); err != nil {
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
	return s.backfill(db, version, hasVersion, allVersions)
}

