package migrator

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// LintFinding represents a single issue discovered in a migration directory.
type LintFinding struct {
	File    string
	Rule    string
	Message string
}

// LintReport contains all findings across the inspected migration files.
type LintReport struct {
	Dir      string
	Findings []LintFinding
	Total    int
}

// HasErrors returns true if any lint findings were discovered.
func (r *LintReport) HasErrors() bool {
	return len(r.Findings) > 0
}

var (
	validMigrationFilenamePattern = regexp.MustCompile(`^(\d+)_([a-zA-Z0-9_-]+)\.(up|down)\.sql$`)
)

// Lint scans migrationsDir and validates that:
// 1. All migration filenames follow `{version}_{name}.up.sql` (or `.down.sql`).
// 2. No two files share the exact same version number prefix (duplicate versions).
// 3. Migration files are not empty.
func Lint(migrationsDir string) (*LintReport, error) {
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("migrations directory %q does not exist", migrationsDir)
		}
		return nil, fmt.Errorf("reading migrations directory %q: %w", migrationsDir, err)
	}

	report := &LintReport{Dir: migrationsDir}
	versionToUpFiles := make(map[uint64][]string)

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") || strings.EqualFold(name, "README.md") {
			continue
		}

		report.Total++
		match := validMigrationFilenamePattern.FindStringSubmatch(name)
		if match == nil {
			report.Findings = append(report.Findings, LintFinding{
				File:    name,
				Rule:    "invalid-filename-format",
				Message: "filename must match '{timestamp}_{name}.up.sql' or '{timestamp}_{name}.down.sql'",
			})
			continue
		}

		verStr := match[1]
		kind := match[3]

		ver, err := strconv.ParseUint(verStr, 10, 64)
		if err != nil {
			report.Findings = append(report.Findings, LintFinding{
				File:    name,
				Rule:    "invalid-version-number",
				Message: fmt.Sprintf("cannot parse version number %q: %v", verStr, err),
			})
			continue
		}

		if kind == "up" {
			versionToUpFiles[ver] = append(versionToUpFiles[ver], name)
		}

		fullPath := filepath.Join(migrationsDir, name)
		content, err := os.ReadFile(fullPath)
		if err != nil {
			return nil, fmt.Errorf("reading file %s: %w", name, err)
		}

		trimmed := strings.TrimSpace(string(content))
		if len(trimmed) == 0 {
			report.Findings = append(report.Findings, LintFinding{
				File:    name,
				Rule:    "empty-migration-file",
				Message: "migration file is empty",
			})
		}
	}

	for ver, files := range versionToUpFiles {
		if len(files) > 1 {
			for _, f := range files {
				report.Findings = append(report.Findings, LintFinding{
					File:    f,
					Rule:    "duplicate-version-number",
					Message: fmt.Sprintf("version %d is duplicated across multiple files: %s", ver, strings.Join(files, ", ")),
				})
			}
		}
	}

	return report, nil
}
