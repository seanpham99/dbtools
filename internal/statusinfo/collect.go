package statusinfo

import "github.com/seanpham99/dbtools/internal/migrator"

// Status is the point-in-time migration state for one target.
type Status struct {
	Target         string   `json:"target"`
	CurrentVersion uint64   `json:"current_version"`
	HasVersion     bool     `json:"has_version"`
	Dirty          bool     `json:"dirty"`
	Pending        []string `json:"pending"`
}

// Collect opens databaseURL, reads its current migration version, and
// diffs it against every migration file in migrationsDir.
func Collect(databaseURL, migrationsDir, targetName string) (*Status, error) {
	m, err := migrator.Open(databaseURL, migrationsDir)
	if err != nil {
		return nil, err
	}
	defer m.Close()

	version, dirty, hasVersion, err := m.Version()
	if err != nil {
		return nil, err
	}

	all, err := ListMigrationFiles(migrationsDir)
	if err != nil {
		return nil, err
	}

	return &Status{
		Target:         targetName,
		CurrentVersion: version,
		HasVersion:     hasVersion,
		Dirty:          dirty,
		Pending:        ComputePending(version, hasVersion, all),
	}, nil
}
