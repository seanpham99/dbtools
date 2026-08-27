package repair

import (
	"database/sql"
	"fmt"
	"os"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/ledger"
	"github.com/seanpham99/dbtools/internal/migrator"
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
func Run(db *sql.DB, eng engine.Engine, migrationsDir, upSuffix, table string, pairs []Pair, force bool) (*Result, error) {
	migrationsDir, upSuffix, table = config.ResolveDefaults(migrationsDir, upSuffix, table)
	if err := eng.Ledger().EnsureSchema(db, table); err != nil {
		return nil, err
	}

	dir, err := migrator.ReadDir(migrationsDir, upSuffix)
	if err != nil {
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
		file, err := dir.Find(pair.Version)
		if err != nil {
			return nil, err
		}
		raw, err := os.ReadFile(file.Path)
		if err != nil {
			return nil, err
		}
		for _, obj := range eng.DDL().ExtractObjects(string(raw)) {
			exists, err := eng.DDL().Exists(db, obj)
			if err != nil {
				return nil, err
			}
			if !exists && !force {
				return nil, fmt.Errorf("version %d (%s): %s.%s does not exist — pass --force to mark it applied anyway", pair.Version, file.Filename, obj.Schema, obj.Name)
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
		if err := eng.Ledger().SetStatus(tx, pair.Version, pair.Status, "repaired via dbtools repair", table); err != nil {
			return nil, err
		}
	}

	applied, err := eng.Ledger().AppliedVersions(tx, table)
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
	// No cursor to stamp: the version is derived from the rows this
	// function just wrote, so recording them IS the stamp.
	newCursor := applied[len(applied)-1]
	result.NewCursor = newCursor
	result.HasCursor = true
	return result, nil
}
