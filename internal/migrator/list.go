package migrator

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
)

var migrationFilenamePattern = regexp.MustCompile(`^(\d+)_.+\.up\.sql$`)

// ListVersions returns every migration version found in migrationsDir,
// parsed from filenames of the form "<version>_<name>.up.sql".
func ListVersions(migrationsDir string) ([]uint64, error) {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("reading migrations dir %q: %w", migrationsDir, err)
	}
	var versions []uint64
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
		versions = append(versions, v)
	}
	return versions, nil
}
