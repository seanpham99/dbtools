package localenv

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const Dir = ".dbtools"

const filename = "local.env"

const gitignoreContents = "*\n"

func Path() string {
	return filepath.Join(Dir, filename)
}

// ensureGitignored writes Dir/.gitignore (contents: "*") if it doesn't
// already exist, so a project that hasn't already ignored Dir doesn't
// accidentally commit the generated local database password Write puts
// in local.env on the next `git add -A`. Self-ignoring — no edit to the
// project's own root .gitignore needed. Never overwrites an existing
// file, in case a user has customized it.
func ensureGitignored() error {
	path := filepath.Join(Dir, ".gitignore")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.WriteFile(path, []byte(gitignoreContents), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func Write(vars map[string]string) error {
	if err := os.MkdirAll(Dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", Dir, err)
	}
	if err := ensureGitignored(); err != nil {
		return err
	}

	var builder strings.Builder
	for key, value := range vars {
		builder.WriteString(key)
		builder.WriteString("=")
		builder.WriteString(value)
		builder.WriteString("\n")
	}

	if err := os.WriteFile(Path(), []byte(builder.String()), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", Path(), err)
	}
	return nil
}

func Load() (map[string]string, error) {
	data, err := os.ReadFile(Path())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", Path(), err)
	}

	vars := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		vars[key] = value
	}
	return vars, nil
}

func Remove() error {
	if err := os.RemoveAll(Dir); err != nil {
		return fmt.Errorf("removing %s: %w", Dir, err)
	}
	return nil
}
