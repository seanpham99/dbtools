// Package projectid derives the identity dbtools uses to scope one
// project's tool-owned local containers and volumes, so two dbtools
// projects on the same machine never collide on a container name.
package projectid

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
)

var nameRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// Resolve returns the project identity for the dbtools.toml at configPath.
// configuredName (dbtools.toml's [project] name field) wins verbatim when
// non-empty and must match Docker's container-name charset. Otherwise the
// identity is the first 8 hex characters of SHA-256(absolute path to
// configPath) — deterministic for a given checkout, no configuration
// needed.
func Resolve(configPath, configuredName string) (string, error) {
	if configuredName != "" {
		if !nameRE.MatchString(configuredName) {
			return "", fmt.Errorf("[project] name %q is invalid: must match %s (Docker container name rules)", configuredName, nameRE.String())
		}
		return configuredName, nil
	}
	abs, err := filepath.Abs(configPath)
	if err != nil {
		return "", fmt.Errorf("resolving absolute path of %s: %w", configPath, err)
	}
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:])[:8], nil
}
