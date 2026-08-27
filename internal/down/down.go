package down

import (
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

	migrationsDir, upSuffix, ledgerTable := config.ResolveDefaults(cfg.MigrationsDir, cfg.Migrations.UpSuffix, cfg.Ledger.Table)

	m, err := migrator.Open(url, migrationsDir)
	if err != nil {
		return nil, err
	}
	defer m.Close()

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

	migrationsDir, upSuffix, ledgerTable := config.ResolveDefaults(cfg.MigrationsDir, cfg.Migrations.UpSuffix, cfg.Ledger.Table)

	m, err := migrator.Open(url, migrationsDir)
	if err != nil {
		return nil, err
	}
	defer m.Close()

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

	versionBefore, dirty, _, err := m.Version()
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}
	if dirty {
		return nil, fmt.Errorf("target %q: migration cursor is dirty at version %d; run `dbtools repair %s` to resolve it", targetName, versionBefore, targetName)
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

	reverted := make([]uint64, 0, len(plan))
	for _, f := range plan {
		stepDone, err := m.StepDown()
		if err != nil {
			return nil, fmt.Errorf("target %q: reverting version %d (%s): %w", targetName, f.Version, f.Filename, err)
		}
		if !stepDone {
			break
		}

		hash, err := dir.DownContentHash(f.Version)
		if err != nil {
			return nil, fmt.Errorf("target %q: %w", targetName, err)
		}

		if err := eng.Ledger().SetStatusWithHash(db, f.Version, ledger.StatusReverted, "reverted via down", hash, ledgerTable); err != nil {
			return nil, fmt.Errorf("target %q: %w", targetName, err)
		}
		reverted = append(reverted, f.Version)
	}

	curVer, _, hasVer, err := m.Version()
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}

	return &Result{
		Target:           targetName,
		RevertedVersions: reverted,
		CurrentVersion:   curVer,
		HasVersion:       hasVer,
	}, nil
}
