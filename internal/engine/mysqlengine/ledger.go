package mysqlengine

import (
	"database/sql"
	"fmt"
	"math"
	"strings"

	"github.com/seanpham99/dbtools/internal/ledger"
)

// mysqlLedgerStore is the MySQL dialect of the dbtools_migration_history
// ledger. Semantics match internal/ledger exactly — only the SQL differs.
type mysqlLedgerStore struct{}

func (mysqlLedgerStore) ensureSchema(db ledger.DBTX, table string) error {
	_, err := db.Exec(fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS %[1]s (
    version         BIGINT       NOT NULL PRIMARY KEY,
    status          VARCHAR(10)  NOT NULL,
    recorded_at     DATETIME     NULL,
    note            VARCHAR(400) NULL,
    content_sha256  CHAR(64)     NULL,
    hash_source     VARCHAR(20)  NULL,
    CHECK (status IN (%[2]s))
) ENGINE=InnoDB`, table, ledger.StatusList()))
	if err != nil {
		return fmt.Errorf("ensuring %s schema: %w", table, err)
	}
	// Column added by dbtools builds before content hashing existed.
	// MySQL's "ADD COLUMN IF NOT EXISTS" is version-gated (8.0.29+), so
	// check information_schema first for broader compatibility — same
	// approach as sqliteengine's pragma_table_info check.
	cols, err := db.Query(fmt.Sprintf(`
SELECT COLUMN_NAME FROM information_schema.columns
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = '%s' AND COLUMN_NAME = 'content_sha256'`, table))
	if err != nil {
		return fmt.Errorf("inspecting %s columns: %w", table, err)
	}
	hasContentHash := cols.Next()
	cols.Close()
	if !hasContentHash {
		if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN content_sha256 CHAR(64) NULL`, table)); err != nil {
			return fmt.Errorf("adding content_sha256 to %s: %w", table, err)
		}
	}
	// Column added for adopt command.
	srcCols, err := db.Query(fmt.Sprintf(`
SELECT COLUMN_NAME FROM information_schema.columns
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = '%s' AND COLUMN_NAME = 'hash_source'`, table))
	if err != nil {
		return fmt.Errorf("inspecting %s columns: %w", table, err)
	}
	hasHashSource := srcCols.Next()
	srcCols.Close()
	if !hasHashSource {
		if _, err := db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN hash_source VARCHAR(20) NULL`, table)); err != nil {
			return fmt.Errorf("adding hash_source to %s: %w", table, err)
		}
	}
	return widenStatusConstraint(db, table)
}

// widenStatusConstraint replaces a pre-v0.7 two-value status CHECK with one
// covering every current status, so an upgraded database can record the
// "applying" state the runner writes before each migration.
//
// MySQL names an unnamed table-level CHECK "<table>_chk_1"; the constraint
// is looked up by parent table rather than assumed, since a hand-created
// ledger may have named it something else.
func widenStatusConstraint(db ledger.DBTX, table string) error {
	rows, err := db.Query(`
SELECT cc.CONSTRAINT_NAME, cc.CHECK_CLAUSE
FROM information_schema.check_constraints cc
JOIN information_schema.table_constraints tc
  ON tc.CONSTRAINT_SCHEMA = cc.CONSTRAINT_SCHEMA AND tc.CONSTRAINT_NAME = cc.CONSTRAINT_NAME
WHERE tc.TABLE_SCHEMA = DATABASE() AND tc.TABLE_NAME = ?`, table)
	if err != nil {
		// check_constraints predates 8.0.16; nothing to widen if the view
		// is absent, because the constraint cannot exist either.
		return nil
	}
	var name, clause string
	found := false
	for rows.Next() {
		var n, c string
		if err := rows.Scan(&n, &c); err != nil {
			rows.Close()
			return fmt.Errorf("inspecting %s constraints: %w", table, err)
		}
		if strings.Contains(c, "status") {
			name, clause, found = n, c, true
		}
	}
	rows.Close()
	if !found || strings.Contains(clause, string(ledger.StatusApplying)) {
		return nil
	}
	// The constraint name comes from information_schema (second-order
	// data): double any backtick so it cannot break out of the identifier.
	name = strings.ReplaceAll(name, "`", "``")
	if _, err := db.Exec(fmt.Sprintf("ALTER TABLE %s DROP CHECK `%s`", table, name)); err != nil {
		return fmt.Errorf("dropping the old status constraint on %s: %w", table, err)
	}
	if _, err := db.Exec(fmt.Sprintf(
		`ALTER TABLE %[1]s ADD CONSTRAINT %[1]s_status_check CHECK (status IN (%[2]s))`,
		table, ledger.StatusList())); err != nil {
		return fmt.Errorf("widening the status constraint on %s: %w", table, err)
	}
	return nil
}

func checkVersionRange(version uint64) error {
	if version > math.MaxInt64 {
		return fmt.Errorf("migration version %d exceeds the ledger's BIGINT range", version)
	}
	return nil
}

// SetStatus upserts version's ledger row, preserving content_sha256 on
// update — the ON DUPLICATE KEY UPDATE clause deliberately omits it.
func (mysqlLedgerStore) SetStatus(db ledger.DBTX, version uint64, status ledger.Status, note, table string) error {
	if err := checkVersionRange(version); err != nil {
		return err
	}
	_, err := db.Exec(fmt.Sprintf(`
INSERT INTO %s (version, status, recorded_at, note)
VALUES (?, ?, NOW(), ?)
ON DUPLICATE KEY UPDATE status = VALUES(status), recorded_at = VALUES(recorded_at), note = VALUES(note)`, table),
		int64(version), string(status), note)
	if err != nil {
		return fmt.Errorf("setting status for version %d: %w", version, err)
	}
	return nil
}

// SetStatusWithHash is SetStatus plus recording the applied migration
// file's content hash, so verify can detect edits after apply.
func (mysqlLedgerStore) SetStatusWithHash(db ledger.DBTX, version uint64, status ledger.Status, note, contentHash, table string) error {
	if err := checkVersionRange(version); err != nil {
		return err
	}
	_, err := db.Exec(fmt.Sprintf(`
INSERT INTO %s (version, status, recorded_at, note, content_sha256)
VALUES (?, ?, NOW(), ?, ?)
ON DUPLICATE KEY UPDATE status = VALUES(status), recorded_at = VALUES(recorded_at), note = VALUES(note), content_sha256 = VALUES(content_sha256), hash_source = ''`, table),
		int64(version), string(status), note, contentHash)
	if err != nil {
		return fmt.Errorf("setting status for version %d: %w", version, err)
	}
	return nil
}

// SetStatusAdopted records version as applied with hash_source "adopted".
func (mysqlLedgerStore) SetStatusAdopted(db ledger.DBTX, version uint64, note, contentHash, table string) error {
	if err := checkVersionRange(version); err != nil {
		return err
	}
	_, err := db.Exec(fmt.Sprintf(`
INSERT INTO %s (version, status, recorded_at, note, content_sha256, hash_source)
VALUES (?, 'applied', NOW(), ?, ?, ?)
ON DUPLICATE KEY UPDATE status = VALUES(status), recorded_at = VALUES(recorded_at), note = VALUES(note), content_sha256 = VALUES(content_sha256), hash_source = VALUES(hash_source)`, table),
		int64(version), note, contentHash, ledger.HashSourceAdopted)
	if err != nil {
		return fmt.Errorf("setting adopted status for version %d: %w", version, err)
	}
	return nil
}

// List returns every ledger row, ordered by version ascending.
func (mysqlLedgerStore) List(db ledger.DBTX, table string) ([]ledger.Entry, error) {
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
func (s mysqlLedgerStore) AppliedVersions(db ledger.DBTX, table string) ([]uint64, error) {
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

// Sync ensures MySQL db's ledger table exists and is backfilled. Refuses
// to backfill when the cursor is dirty (a previous apply failed partway).
func (s mysqlLedgerStore) EnsureSchema(db ledger.DBTX, table string) error {
	return s.ensureSchema(db, table)
}

// State derives the migration state from the ledger's own rows. The SQL is
// identical on every engine, so it lives in the ledger package rather than
// as four copies that could drift.
func (mysqlLedgerStore) State(db ledger.DBTX, table string) (ledger.State, error) {
	return ledger.QueryState(db, table)
}
