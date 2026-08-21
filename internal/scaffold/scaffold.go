package scaffold

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var migrationFilenamePattern = regexp.MustCompile(`^(\d+)_.+\.up\.sql$`)

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
	clockVer, err := strconv.ParseUint(now.UTC().Format("20060102150405"), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("formatting clock version: %w", err)
	}

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return clockVer, nil
		}
		return 0, fmt.Errorf("reading migrations dir %q: %w", migrationsDir, err)
	}

	var maxExisting uint64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := migrationFilenamePattern.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		v, err := strconv.ParseUint(m[1], 10, 64)
		if err != nil {
			continue
		}
		if v > maxExisting {
			maxExisting = v
		}
	}

	if maxExisting >= clockVer {
		return maxExisting + 1, nil
	}
	return clockVer, nil
}

// NextUpFilename computes the next migration filename for the given name in migrationsDir.
func NextUpFilename(now time.Time, migrationsDir, name string) (string, error) {
	ver, err := NextVersion(now, migrationsDir)
	if err != nil {
		return "", err
	}
	slug := strings.ReplaceAll(strings.TrimSpace(name), " ", "_")
	return fmt.Sprintf("%014d_%s.up.sql", ver, slug), nil
}
