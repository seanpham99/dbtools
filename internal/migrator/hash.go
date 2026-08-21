package migrator

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// ContentHash returns the hex SHA-256 of a migration file's contents. It
// is what the ledger stores when a migration is applied, so verify can
// detect an applied migration being edited after the fact — the most
// common real-world schema drift.
func ContentHash(migrationsDir string, version uint64) (string, error) {
	filename, err := FindMigrationFile(migrationsDir, version)
	if err != nil {
		return "", err
	}
	raw, err := os.ReadFile(filepath.Join(migrationsDir, filename))
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", filename, err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
