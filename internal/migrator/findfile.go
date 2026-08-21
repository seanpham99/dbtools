package migrator

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// FindMigrationFile returns the filename (not full path) of the .up.sql
// migration file for version in migrationsDir, or an error if no file
// matches.
func FindMigrationFile(migrationsDir string, version uint64) (string, error) {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return "", fmt.Errorf("reading migrations dir %q: %w", migrationsDir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := migrationFilenamePattern.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		v, err := strconv.ParseUint(m[1], 10, 64)
		if err != nil || v != version {
			continue
		}
		if !strings.HasSuffix(e.Name(), ".up.sql") {
			continue // a .down.sql file for this version — not what callers want
		}
		return e.Name(), nil
	}
	return "", fmt.Errorf("version %d does not match any migration file in %s", version, migrationsDir)
}
