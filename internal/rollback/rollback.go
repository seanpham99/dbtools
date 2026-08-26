package rollback

import (
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
func Run(cfg *config.Config, targetName string, steps int, urlOverride string) (*Result, error) {
	url, err := cfg.ResolveURLOrFlag(targetName, urlOverride)
	if err != nil {
		return nil, err
	}

	eng, err := engine.ForTarget(cfg.EngineName(targetName), url)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}

	m, err := migrator.Open(url, cfg.MigrationsDir)
	if err != nil {
		return nil, err
	}
	defer m.Close()

	db, err := eng.Open(url)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}
	defer db.Close()

	if err := eng.Ledger().Sync(db, m, cfg.MigrationsDir, cfg.Migrations.UpSuffix); err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}

	applied, err := eng.Ledger().AppliedVersions(db)
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
		if err := eng.Ledger().SetStatus(tx, ver, ledger.StatusReverted, "soft-reverted via rollback"); err != nil {
			return nil, err
		}
	}

	remaining, err := eng.Ledger().AppliedVersions(tx)
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
		newCursor := remaining[len(remaining)-1]
		if err := m.Stamp(newCursor); err != nil {
			return nil, err
		}
		result.NewCursor = newCursor
		result.HasCursor = true
	}

	return result, nil
}
