package rollback

import (
	"context"
	"fmt"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/ledger"
	"github.com/seanpham99/dbtools/internal/migrator"
)

// Result summarizes what rollback.Run executed.
type Result struct {
	Target           string   `json:"target"`
	RevertedVersions []uint64 `json:"reverted_versions"`
	NewCursor        uint64   `json:"new_cursor"`
	HasCursor        bool     `json:"has_cursor"`
}

// Run performs a ledger-only soft-revert for targetName up to steps count.
// It marks versions as StatusReverted in the ledger and recomputes the
// migration cursor without executing any .down.sql files or dropping objects.
func Run(cfg *config.Config, targetName string, steps int, urlOverride string) (result *Result, err error) {
	return runLocked(cfg, targetName, steps, urlOverride)
}

func runLocked(cfg *config.Config, targetName string, steps int, urlOverride string) (*Result, error) {
	url, err := cfg.ResolveURLOrFlag(targetName, urlOverride)
	if err != nil {
		return nil, err
	}

	eng, err := engine.ForTarget(cfg.EngineName(targetName), url)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}

	migrationsDir, upSuffix, ledgerTable := config.ResolveDefaults(cfg.MigrationsDir, cfg.Migrations.UpSuffix, cfg.LedgerTableName())

	db, err := eng.Open(url)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}
	defer db.Close()

	if err := eng.Ledger().EnsureSchema(db, ledgerTable); err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}

	// Rollback rewrites ledger rows without running SQL, so it takes the
	// migration lock and refuses a mid-apply ledger for the same reason
	// the runner does: marking a version reverted while its migration is
	// still executing would erase the only record that it started.
	dir, err := migrator.ReadDir(migrationsDir, upSuffix)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}
	runner := migrator.NewRunner(eng, db, dir, ledgerTable)
	release, err := runner.LockForWrite(context.Background())
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}
	defer release()

	state, err := eng.Ledger().State(db, ledgerTable)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}
	if state.Dirty {
		return nil, &ledger.DirtyError{Version: state.Applying, Table: ledgerTable}
	}

	applied, err := eng.Ledger().AppliedVersions(db, ledgerTable)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}

	if len(applied) == 0 {
		return &Result{
			Target:           targetName,
			RevertedVersions: nil,
			NewCursor:        0,
			HasCursor:        false,
		}, nil
	}

	count := steps
	if count <= 0 || count > len(applied) {
		count = 1
	}

	reverted := make([]uint64, 0, count)
	for i := len(applied) - 1; i >= len(applied)-count; i-- {
		reverted = append(reverted, applied[i])
	}

	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	for _, ver := range reverted {
		if err := eng.Ledger().SetStatus(tx, ver, ledger.StatusReverted, "soft-reverted via rollback", ledgerTable); err != nil {
			return nil, err
		}
	}

	remaining, err := eng.Ledger().AppliedVersions(tx, ledgerTable)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	result := &Result{
		Target:           targetName,
		RevertedVersions: reverted,
	}

	if len(remaining) > 0 {
		// Derived from the ledger rows above, not stamped separately.
		newCursor := remaining[len(remaining)-1]
		result.NewCursor = newCursor
		result.HasCursor = true
	}

	return result, nil
}
