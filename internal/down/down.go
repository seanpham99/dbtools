package down

import (
	"context"
	"fmt"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/ledger"
	"github.com/seanpham99/dbtools/internal/migrator"
)

// Result summarizes what down.Run executed.
type Result struct {
	Target           string   `json:"target"`
	RevertedVersions []uint64 `json:"reverted_versions"`
	CurrentVersion   uint64   `json:"current_version"`
	HasVersion       bool     `json:"has_version"`
}

// Preview returns the list of down migration files that would be executed for targetName
// without modifying the database or ledger.
func Preview(cfg *config.Config, targetName string, steps int, urlOverride string) ([]migrator.File, error) {
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

	dir, err := migrator.ReadDir(migrationsDir, upSuffix)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}

	applied, err := eng.Ledger().AppliedVersions(db, ledgerTable)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}

	return dir.DownPlan(applied, steps)
}

// Run executes down migrations for targetName in reverse order up to steps count.
// Each reverted version is recorded in the ledger with its .down.sql content hash.
func Run(cfg *config.Config, targetName string, steps int, urlOverride string) (*Result, error) {
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

	dir, err := migrator.ReadDir(migrationsDir, upSuffix)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}

	applied, err := eng.Ledger().AppliedVersions(db, ledgerTable)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}

	if len(applied) == 0 {
		return &Result{
			Target:           targetName,
			RevertedVersions: nil,
			CurrentVersion:   0,
			HasVersion:       false,
		}, nil
	}

	plan, err := dir.DownPlan(applied, steps)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}

	// The runner holds the migration lock for the whole revert and refuses
	// to start if a previous run left a migration mid-apply.
	runner := migrator.NewRunner(eng, db, dir, ledgerTable)
	n, err := runner.Down(context.Background(), steps)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}
	reverted := make([]uint64, 0, n)
	for i := 0; i < n && i < len(plan); i++ {
		f := plan[i]
		hash, hErr := dir.DownContentHash(f.Version)
		if hErr == nil {
			_ = eng.Ledger().SetStatusWithHash(db, f.Version, ledger.StatusReverted, "reverted via down", hash, ledgerTable)
		}
		reverted = append(reverted, f.Version)
	}

	state, err := runner.State(context.Background())
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}
	curVer, hasVer := state.Version, state.HasVersion

	return &Result{
		Target:           targetName,
		RevertedVersions: reverted,
		CurrentVersion:   curVer,
		HasVersion:       hasVer,
	}, nil
}
