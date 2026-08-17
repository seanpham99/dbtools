package statusinfo

import (
	"os"
	"sort"
	"strconv"
	"strings"
)

// MigrationFile is one plain-SQL migration file on disk.
type MigrationFile struct {
	Version  uint64
	Filename string
}

// ListMigrationFiles reads migrationsDir and returns every *.up.sql file,
// sorted ascending by version.
func ListMigrationFiles(migrationsDir string) ([]MigrationFile, error) {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return nil, err
	}

	var files []MigrationFile
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		versionStr, _, found := strings.Cut(name, "_")
		if !found {
			continue
		}
		version, err := strconv.ParseUint(versionStr, 10, 64)
		if err != nil {
			continue
		}
		files = append(files, MigrationFile{Version: version, Filename: name})
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Version < files[j].Version })
	return files, nil
}

// ComputePending returns the filenames of every migration whose version is
// greater than currentVersion, in ascending order. If hasVersion is false
// (no migration ever applied), every file is pending.
func ComputePending(currentVersion uint64, hasVersion bool, all []MigrationFile) []string {
	var pending []string
	for _, f := range all {
		if !hasVersion || f.Version > currentVersion {
			pending = append(pending, f.Filename)
		}
	}
	return pending
}

// ComputePendingVersions returns the versions (not filenames) of every
// migration whose version is greater than currentVersion, in ascending
// order. Used by callers (like internal/apply) that need to act on
// specific versions rather than just display filenames.
func ComputePendingVersions(currentVersion uint64, hasVersion bool, all []MigrationFile) []uint64 {
	var pending []uint64
	for _, f := range all {
		if !hasVersion || f.Version > currentVersion {
			pending = append(pending, f.Version)
		}
	}
	return pending
}
