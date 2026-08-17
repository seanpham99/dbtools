package repair

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dbtools/dbtools/internal/engine"
	"github.com/dbtools/dbtools/internal/ledger"
	"github.com/dbtools/dbtools/internal/migrator"
)

// Pair is one version's requested ledger status change.
type Pair struct {
	Version uint64
	Status  ledger.Status
}

// Result summarizes what Run did.
type Result struct {
	Repaired  []Pair
	NewCursor uint64
	HasCursor bool // false if no version remains marked applied
}

// Run syncs the ledger, validates every pair (refusing to mark a version
// applied when its migration's objects don't exist unless force is set),
// applies all pairs, and recomputes db's cursor as the highest remaining
// applied version.
func Run(db *sql.DB, eng engine.Engine, m *migrator.Migrator, migrationsDir string, pairs []Pair, force bool) (*Result, error) {
	if err := eng.Ledger().Sync(db, m, migrationsDir); err != nil {
		return nil, err
	}

	// Validate every pair before writing anything, so a bad pair never
	// leaves the ledger partially repaired.
	for _, pair := range pairs {
		// Marking a version reverted doesn't need its file to still exist —
		// this is exactly how a renamed/superseded migration's stale ledger
		// row gets cleared, and there are no objects to check without a file.
		if pair.Status != ledger.StatusApplied {
			continue
		}
		filename, err := migrator.FindMigrationFile(migrationsDir, pair.Version)
		if err != nil {
			return nil, err
		}
		raw, err := os.ReadFile(filepath.Join(migrationsDir, filename))
		if err != nil {
			return nil, err
		}
		for _, obj := range eng.DDL().ExtractObjects(string(raw)) {
			exists, err := eng.DDL().Exists(db, obj)
			if err != nil {
				return nil, err
			}
			if !exists && !force {
				return nil, fmt.Errorf("version %d (%s): %s.%s does not exist — pass --force to mark it applied anyway", pair.Version, filename, obj.Schema, obj.Name)
			}
		}
	}

	// All writes plus the resulting AppliedVersions read happen in one
	// transaction, so a mid-loop failure (e.g. a dropped connection) leaves
	// no pairs half-written — either every requested pair lands, or none do.
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	for _, pair := range pairs {
		if err := eng.Ledger().SetStatus(tx, pair.Version, pair.Status, "repaired via dbtools repair"); err != nil {
			return nil, err
		}
	}

	applied, err := eng.Ledger().AppliedVersions(tx)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	result := &Result{Repaired: pairs}
	if len(applied) == 0 {
		return result, nil
	}
	newCursor := applied[len(applied)-1]
	if err := m.Stamp(newCursor); err != nil {
		return nil, err
	}
	result.NewCursor = newCursor
	result.HasCursor = true
	return result, nil
}
