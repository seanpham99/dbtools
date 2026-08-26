package migrator

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	validDownFilenamePattern = regexp.MustCompile(`^(\d+)_.+\.down\.sql$`)
)

// upFilenamePattern compiles the up-migration filename pattern for the
// given suffix, e.g. ".up.sql" -> `^(\d+)_.+\.up\.sql$`, ".sql" -> `^(\d+)_.+\.sql$`.
func upFilenamePattern(upSuffix string) *regexp.Regexp {
	if upSuffix == "" {
		upSuffix = ".up.sql"
	}
	return regexp.MustCompile(`^(\d+)_.+` + regexp.QuoteMeta(upSuffix) + `$`)
}

// File represents one plain-SQL migration file on disk.
type File struct {
	Version  uint64 `json:"version"`
	Filename string `json:"filename"`
	Path     string `json:"path"`
}

// Dir is an in-memory indexed view of a local migrations directory.
type Dir struct {
	path          string
	upSuffix      string
	upFiles       []File
	downFiles     []File
	byVersion     map[uint64]File
	byVersionDown map[uint64]File
}

// ReadDir scans migrationsDir, parses version numbers, sorts ascending, and indexes
// all migration files matching upSuffix and *.down.sql in memory.
func ReadDir(migrationsDir, upSuffix string) (*Dir, error) {
	if upSuffix == "" {
		upSuffix = ".up.sql"
	}
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return &Dir{
				path:          migrationsDir,
				upSuffix:      upSuffix,
				upFiles:       nil,
				downFiles:     nil,
				byVersion:     make(map[uint64]File),
				byVersionDown: make(map[uint64]File),
			}, nil
		}
		return nil, fmt.Errorf("reading migrations dir %q: %w", migrationsDir, err)
	}

	var upFiles []File
	var downFiles []File
	byVersion := make(map[uint64]File)
	byVersionDown := make(map[uint64]File)
	upPat := upFilenamePattern(upSuffix)

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if m := validDownFilenamePattern.FindStringSubmatch(name); m != nil {
			ver, err := strconv.ParseUint(m[1], 10, 64)
			if err != nil {
				continue
			}
			f := File{
				Version:  ver,
				Filename: name,
				Path:     filepath.Join(migrationsDir, name),
			}
			downFiles = append(downFiles, f)
			byVersionDown[ver] = f
		} else if m := upPat.FindStringSubmatch(name); m != nil {
			if upSuffix != ".up.sql" && strings.HasSuffix(name, ".up.sql") {
				continue
			}
			ver, err := strconv.ParseUint(m[1], 10, 64)
			if err != nil {
				continue
			}
			f := File{
				Version:  ver,
				Filename: name,
				Path:     filepath.Join(migrationsDir, name),
			}
			upFiles = append(upFiles, f)
			byVersion[ver] = f
		}
	}

	sort.Slice(upFiles, func(i, j int) bool {
		return upFiles[i].Version < upFiles[j].Version
	})
	sort.Slice(downFiles, func(i, j int) bool {
		return downFiles[i].Version < downFiles[j].Version
	})

	return &Dir{
		path:          migrationsDir,
		upSuffix:      upSuffix,
		upFiles:       upFiles,
		downFiles:     downFiles,
		byVersion:     byVersion,
		byVersionDown: byVersionDown,
	}, nil
}

// List returns every *.up.sql migration file in ascending order.
func (d *Dir) List() []File {
	result := make([]File, len(d.upFiles))
	copy(result, d.upFiles)
	return result
}

// ListDown returns every *.down.sql migration file in ascending order.
func (d *Dir) ListDown() []File {
	result := make([]File, len(d.downFiles))
	copy(result, d.downFiles)
	return result
}

// ListVersions returns every up migration version in ascending order.
func (d *Dir) ListVersions() []uint64 {
	versions := make([]uint64, len(d.upFiles))
	for i, f := range d.upFiles {
		versions[i] = f.Version
	}
	return versions
}

// Find returns the *.up.sql File matching version, or an error if not found.
func (d *Dir) Find(version uint64) (File, error) {
	if f, ok := d.byVersion[version]; ok {
		return f, nil
	}
	return File{}, fmt.Errorf("version %d does not match any migration file in %s", version, d.path)
}

// FindDown returns the *.down.sql File matching version, or an error if not found.
func (d *Dir) FindDown(version uint64) (File, error) {
	if f, ok := d.byVersionDown[version]; ok {
		return f, nil
	}
	return File{}, fmt.Errorf("version %d does not have a matching .down.sql migration file in %s", version, d.path)
}

// PendingAfter returns the files for every migration whose version is greater than
// currentVersion, ascending. If hasVersion is false, all files are returned.
func (d *Dir) PendingAfter(currentVersion uint64, hasVersion bool) []File {
	var pending []File
	for _, f := range d.upFiles {
		if !hasVersion || f.Version > currentVersion {
			pending = append(pending, f)
		}
	}
	return pending
}

// PendingFilenames returns only the filenames of pending migrations.
func (d *Dir) PendingFilenames(currentVersion uint64, hasVersion bool) []string {
	pending := d.PendingAfter(currentVersion, hasVersion)
	filenames := make([]string, len(pending))
	for i, f := range pending {
		filenames[i] = f.Filename
	}
	return filenames
}

// PendingVersions returns only the version numbers of pending migrations.
func (d *Dir) PendingVersions(currentVersion uint64, hasVersion bool) []uint64 {
	pending := d.PendingAfter(currentVersion, hasVersion)
	versions := make([]uint64, len(pending))
	for i, f := range pending {
		versions[i] = f.Version
	}
	return versions
}

// DownPlan returns the list of down migration files to apply in reverse order (newest first)
// for the given applied versions (which are in ascending order), up to steps count.
// If steps <= 0 or steps >= len(appliedVersions), all applied versions are planned.
func (d *Dir) DownPlan(appliedVersions []uint64, steps int) ([]File, error) {
	if len(appliedVersions) == 0 {
		return nil, nil
	}

	count := steps
	if count <= 0 || count > len(appliedVersions) {
		count = len(appliedVersions)
	}

	plan := make([]File, 0, count)
	// Iterate backwards from the latest applied version
	for i := len(appliedVersions) - 1; i >= len(appliedVersions)-count; i-- {
		ver := appliedVersions[i]
		downFile, err := d.FindDown(ver)
		if err != nil {
			return nil, err
		}
		plan = append(plan, downFile)
	}
	return plan, nil
}

// NextVersion computes max(clock_version, max_existing_version + 1), ensuring
// newly scaffolded migrations are never created behind the highest version present.
func (d *Dir) NextVersion(now time.Time) (uint64, error) {
	clockVer, err := strconv.ParseUint(now.UTC().Format("20060102150405"), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("formatting clock version: %w", err)
	}

	var maxExisting uint64
	for _, f := range d.upFiles {
		if f.Version > maxExisting {
			maxExisting = f.Version
		}
	}

	if maxExisting >= clockVer {
		return maxExisting + 1, nil
	}
	return clockVer, nil
}

// NextUpFilename computes the next migration filename for the given name.
func (d *Dir) NextUpFilename(now time.Time, name string) (string, error) {
	ver, err := d.NextVersion(now)
	if err != nil {
		return "", err
	}
	slug := strings.ReplaceAll(strings.TrimSpace(name), " ", "_")
	suffix := d.upSuffix
	if suffix == "" {
		suffix = ".up.sql"
	}
	return fmt.Sprintf("%014d_%s%s", ver, slug, suffix), nil
}

// ContentHash computes the SHA-256 hex string of version's *.up.sql file.
func (d *Dir) ContentHash(version uint64) (string, error) {
	f, err := d.Find(version)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(f.Path)
	if err != nil {
		return "", fmt.Errorf("reading migration file %s: %w", f.Filename, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// DownContentHash computes the SHA-256 hex string of version's *.down.sql file.
func (d *Dir) DownContentHash(version uint64) (string, error) {
	f, err := d.FindDown(version)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(f.Path)
	if err != nil {
		return "", fmt.Errorf("reading down migration file %s: %w", f.Filename, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// ListVersions returns every migration version found in migrationsDir matching upSuffix, ascending.
func ListVersions(migrationsDir, upSuffix string) ([]uint64, error) {
	d, err := ReadDir(migrationsDir, upSuffix)
	if err != nil {
		return nil, err
	}
	return d.ListVersions(), nil
}

// FindMigrationFile returns the filename of the up migration file for version in migrationsDir.
func FindMigrationFile(migrationsDir, upSuffix string, version uint64) (string, error) {
	d, err := ReadDir(migrationsDir, upSuffix)
	if err != nil {
		return "", err
	}
	f, err := d.Find(version)
	if err != nil {
		return "", err
	}
	return f.Filename, nil
}
