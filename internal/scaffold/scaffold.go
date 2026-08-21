package scaffold

import (
	"strings"
	"time"

	"github.com/seanpham99/dbtools/internal/migrator"
)

// UpFilename returns the filename for a new migration created at `now`
// with the given human-readable name, e.g. "20260701041134_add_widget.up.sql".
func UpFilename(now time.Time, name string) string {
	slug := strings.ReplaceAll(strings.TrimSpace(name), " ", "_")
	return now.UTC().Format("20060102150405") + "_" + slug + ".up.sql"
}

// NextVersion returns the next migration version number to use.
// It computes max(clock_version, max_existing_version + 1), ensuring that
// newly scaffolded migrations are never created behind the highest version
// already present in migrationsDir (e.g. from skewed clocks or future-dated migrations).
func NextVersion(now time.Time, migrationsDir string) (uint64, error) {
	d, err := migrator.ReadDir(migrationsDir)
	if err != nil {
		return 0, err
	}
	return d.NextVersion(now)
}

// NextUpFilename computes the next migration filename for the given name in migrationsDir.
func NextUpFilename(now time.Time, migrationsDir, name string) (string, error) {
	d, err := migrator.ReadDir(migrationsDir)
	if err != nil {
		return "", err
	}
	return d.NextUpFilename(now, name)
}
