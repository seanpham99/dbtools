// Package adopt provides detection, reading, diffing, and adoption logic
// for preexisting database migration history tables into dbtools.
package adopt

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/seanpham99/dbtools/internal/ledger"
	"github.com/seanpham99/dbtools/internal/migrator"
)

// KnownTable describes a known third-party migration tracking table.
type KnownTable struct {
	Name          string
	VersionCol    string
	AppliedAtCol  string
}

// KnownSourceTables lists the supported third-party migration tracking tables
// in probe order.
var KnownSourceTables = []KnownTable{
	{Name: "schema_migrations", VersionCol: "version", AppliedAtCol: ""},
	{Name: "flyway_schema_history", VersionCol: "version", AppliedAtCol: "installed_on"},
	{Name: "__EFMigrationsHistory", VersionCol: "MigrationId", AppliedAtCol: "AppliedDate"},
	{Name: "knex_migrations", VersionCol: "name", AppliedAtCol: "migration_time"},
	{Name: "alembic_version", VersionCol: "version_num", AppliedAtCol: ""},
	{Name: "SchemaVersions", VersionCol: "ScriptName", AppliedAtCol: "Applied"},
}

// KnownTableNames returns the names of the known candidate tables.
func KnownTableNames() []string {
	names := make([]string, len(KnownSourceTables))
	for i, kt := range KnownSourceTables {
		names[i] = kt.Name
	}
	return names
}

// DefaultColumnsForTable returns the default version and applied-at column names
// for a known source table, or defaults to ("version", "") if unknown.
func DefaultColumnsForTable(tableName string) (versionCol, appliedAtCol string) {
	for _, kt := range KnownSourceTables {
		if strings.EqualFold(kt.Name, tableName) {
			return kt.VersionCol, kt.AppliedAtCol
		}
	}
	return "version", ""
}

// SourceRow is one migration record read from a third-party migration table.
type SourceRow struct {
	Version   uint64
	AppliedAt *time.Time
}

// Plan is the 3-way diff between source table rows and on-disk migration files.
type Plan struct {
	SourceTable string   `json:"source_table"`
	Matched     []uint64 `json:"matched"`
	Pending     []uint64 `json:"pending"`
	Orphan      []uint64 `json:"orphan"`
}

// DetectSourceTable probes candidateTables in order via existsFunc
// and returns the first matching table name, or an error if none match.
func DetectSourceTable(db ledger.DBTX, existsFunc func(ledger.DBTX, string) (bool, error), candidateTables []string) (string, error) {
	for _, tbl := range candidateTables {
		exists, err := existsFunc(db, tbl)
		if err != nil {
			return "", fmt.Errorf("checking existence of table %q: %w", tbl, err)
		}
		if exists {
			return tbl, nil
		}
	}
	return "", fmt.Errorf("no candidate migration table found among [%s]; specify --from-table <name> --version-column <col>", strings.Join(candidateTables, ", "))
}

var leadingDigitsPattern = regexp.MustCompile(`^(\d+)`)

// parseVersion extracts a uint64 version from a database value (numeric or string).
func parseVersion(v any) (uint64, error) {
	switch val := v.(type) {
	case int64:
		if val < 0 {
			return 0, fmt.Errorf("negative version number: %d", val)
		}
		return uint64(val), nil
	case int:
		if val < 0 {
			return 0, fmt.Errorf("negative version number: %d", val)
		}
		return uint64(val), nil
	case uint64:
		return val, nil
	case []byte:
		return parseVersionString(string(val))
	case string:
		return parseVersionString(val)
	default:
		return parseVersionString(fmt.Sprint(v))
	}
}

func parseVersionString(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	// Try parsing full string as uint64 first
	if u, err := strconv.ParseUint(s, 10, 64); err == nil {
		return u, nil
	}
	// Extract leading digits (e.g. "20260822000001_create_users" or "V1__initial")
	clean := strings.TrimPrefix(s, "V")
	clean = strings.TrimPrefix(clean, "v")
	match := leadingDigitsPattern.FindString(clean)
	if match != "" {
		if u, err := strconv.ParseUint(match, 10, 64); err == nil {
			return u, nil
		}
	}
	return 0, fmt.Errorf("cannot parse version number from %q", s)
}

// ReadSourceRows reads rows from table using the specified version and applied-at columns.
func ReadSourceRows(db ledger.DBTX, table, versionColumn, appliedAtColumn string) ([]SourceRow, error) {
	if err := ledger.ValidateTableName(table); err != nil {
		return nil, err
	}
	if err := ledger.ValidateTableName(versionColumn); err != nil {
		return nil, err
	}
	if appliedAtColumn != "" {
		if err := ledger.ValidateTableName(appliedAtColumn); err != nil {
			return nil, err
		}
	}

	query := fmt.Sprintf("SELECT %s FROM %s", versionColumn, table)
	if appliedAtColumn != "" {
		query = fmt.Sprintf("SELECT %s, %s FROM %s", versionColumn, appliedAtColumn, table)
	}

	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("querying %s: %w", table, err)
	}
	defer rows.Close()

	var result []SourceRow
	for rows.Next() {
		var rawVersion any
		var rawAppliedAt any
		if appliedAtColumn != "" {
			if err := rows.Scan(&rawVersion, &rawAppliedAt); err != nil {
				return nil, fmt.Errorf("scanning %s row: %w", table, err)
			}
		} else {
			if err := rows.Scan(&rawVersion); err != nil {
				return nil, fmt.Errorf("scanning %s row: %w", table, err)
			}
		}

		if rawVersion == nil {
			continue
		}

		ver, err := parseVersion(rawVersion)
		if err != nil {
			return nil, fmt.Errorf("parsing version from %s: %w", table, err)
		}

		sr := SourceRow{Version: ver}
		if rawAppliedAt != nil {
			switch at := rawAppliedAt.(type) {
			case time.Time:
				sr.AppliedAt = &at
			case *time.Time:
				sr.AppliedAt = at
			}
		}
		result = append(result, sr)
	}

	return result, rows.Err()
}

// BuildPlan constructs a 3-way diff between source table rows and files on disk.
func BuildPlan(sourceTable string, sourceRows []SourceRow, dir *migrator.Dir) Plan {
	diskVersions := make(map[uint64]bool)
	if dir != nil {
		for _, v := range dir.ListVersions() {
			diskVersions[v] = true
		}
	}

	srcVersions := make(map[uint64]bool)
	for _, r := range sourceRows {
		srcVersions[r.Version] = true
	}

	var matched []uint64
	var pending []uint64
	var orphan []uint64

	// Collect union of all versions
	allVersionsMap := make(map[uint64]bool)
	for v := range diskVersions {
		allVersionsMap[v] = true
	}
	for v := range srcVersions {
		allVersionsMap[v] = true
	}

	var allVersions []uint64
	for v := range allVersionsMap {
		allVersions = append(allVersions, v)
	}
	sort.Slice(allVersions, func(i, j int) bool { return allVersions[i] < allVersions[j] })

	for _, v := range allVersions {
		inDisk := diskVersions[v]
		inSrc := srcVersions[v]
		switch {
		case inDisk && inSrc:
			matched = append(matched, v)
		case inDisk && !inSrc:
			pending = append(pending, v)
		case !inDisk && inSrc:
			orphan = append(orphan, v)
		}
	}

	return Plan{
		SourceTable: sourceTable,
		Matched:     matched,
		Pending:     pending,
		Orphan:      orphan,
	}
}
