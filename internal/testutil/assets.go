package testutil

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed testdata
var testdataFS embed.FS

// ExtractMigrations copies the embedded migration files for dialect ("sqlite", "postgres", "mssql")
// into destDir on disk.
func ExtractMigrations(destDir, dialect string) error {
	subPath := fmt.Sprintf("testdata/classicmodels/%s", dialect)
	entries, err := fs.ReadDir(testdataFS, subPath)
	if err != nil {
		return fmt.Errorf("reading embedded migrations for %s: %w", dialect, err)
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("creating dest dir: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := testdataFS.ReadFile(filepath.Join(subPath, e.Name()))
		if err != nil {
			return fmt.Errorf("reading embedded migration %s: %w", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(destDir, e.Name()), data, 0o644); err != nil {
			return fmt.Errorf("writing migration file %s: %w", e.Name(), err)
		}
	}
	return nil
}

// GetSeedSQL returns the content of seed.sql.
func GetSeedSQL() (string, error) {
	data, err := testdataFS.ReadFile("testdata/seed.sql")
	if err != nil {
		return "", fmt.Errorf("reading seed.sql: %w", err)
	}
	return string(data), nil
}

// ReadGolden returns the committed golden file content for dialect and lang ("python" -> "models.py", "ts" -> "models.ts").
func ReadGolden(dialect, lang string) (string, error) {
	filename := "models.py"
	if lang == "ts" {
		filename = "models.ts"
	}
	path := fmt.Sprintf("testdata/golden/%s/%s", dialect, filename)
	data, err := testdataFS.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading golden file %s: %w", path, err)
	}
	return string(data), nil
}

// StripGeneratedHeader strips the dynamic timestamp comment header from generated files so
// they can be compared stably.
func StripGeneratedHeader(content string) string {
	lines := strings.Split(content, "\n")
	var kept []string
	for _, l := range lines {
		if strings.HasPrefix(l, "# Code generated") || strings.HasPrefix(l, "# Source target") ||
			strings.HasPrefix(l, "// Code generated") || strings.HasPrefix(l, "// Source target") {
			continue
		}
		kept = append(kept, l)
	}
	return strings.Join(kept, "\n")
}
