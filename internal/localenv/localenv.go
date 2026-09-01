package localenv

import (
	"errors"
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
// file, in case a user has customized it — created with O_EXCL so a
// concurrent dbtools process racing to create the same file loses the
// write cleanly (ErrExist) rather than one process's WriteFile silently
// truncating the other's already-written content.
func ensureGitignored() error {
	path := filepath.Join(Dir, ".gitignore")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return fmt.Errorf("writing %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.WriteString(gitignoreContents); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func Write(vars map[string]string) error {
	// Refuse to write through a symlinked .dbtools: a planted symlink would
	// redirect the generated local URL (which carries a credential) to an
	// attacker-chosen target.
	if info, err := os.Lstat(Dir); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink; refusing to write %s", Dir, Path())
	}
	if err := os.MkdirAll(Dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", Dir, err)
	}
	if err := ensureGitignored(); err != nil {
		return err
	}

	var builder strings.Builder
	for key, value := range vars {
		if strings.ContainsAny(value, "\n\r") {
			return fmt.Errorf("value for %s contains a newline; refusing to write it to %s", key, Path())
		}
		builder.WriteString(key)
		builder.WriteString("=")
		builder.WriteString(value)
		builder.WriteString("\n")
	}

	// Refuse a symlinked local.env — a planted symlink would redirect the
	// credential-bearing URL to an attacker-chosen target. (Lstat pre-check
	// rather than O_NOFOLLOW, which the os package does not expose
	// portably; the race window here is not part of the threat model.)
	if info, err := os.Lstat(Path()); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink; refusing to write it", Path())
	}
	// The mode only applies at creation, so re-open with explicit perms and
	// re-Chmod to enforce 0600 on a pre-existing file that a permissive
	// umask or backup restore loosened.
	f, err := os.OpenFile(Path(), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("writing %s: %w", Path(), err)
	}
	defer f.Close()
	if err := f.Chmod(0o600); err != nil {
		return fmt.Errorf("enforcing 0600 on %s: %w", Path(), err)
	}
	if _, err := f.WriteString(builder.String()); err != nil {
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
