package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/migrator"
)

type dryRunMigration struct {
	Version  uint64 `json:"version"`
	Filename string `json:"filename"`
	SQL      string `json:"sql"`
}

type dryRunResult struct {
	Target  string            `json:"target"`
	DryRun  bool              `json:"dry_run"`
	Pending []dryRunMigration `json:"pending"`
}

func runDryRun(cfg *config.Config, targetName, urlOverride string) error {
	url, err := cfg.ResolveURLOrFlag(targetName, urlOverride)
	if err != nil {
		return err
	}

	if _, err := engine.ForTarget(cfg.EngineName(targetName), url); err != nil {
		return err
	}

	m, err := migrator.Open(url, cfg.MigrationsDir)
	if err != nil {
		return err
	}
	defer m.Close()

	curVer, dirty, hasVer, err := m.Version()
	if err != nil {
		return err
	}
	if dirty {
		return fmt.Errorf("target %q: migration cursor is dirty at version %d; run `dbtools repair %s` to resolve it", targetName, curVer, targetName)
	}

	dir, err := migrator.ReadDir(cfg.MigrationsDir, cfg.Migrations.UpSuffix)
	if err != nil {
		return err
	}

	pending := dir.PendingAfter(curVer, hasVer)

	items := make([]dryRunMigration, 0, len(pending))
	for _, f := range pending {
		sqlBytes, err := os.ReadFile(f.Path)
		if err != nil {
			return fmt.Errorf("reading migration file %s: %w", f.Filename, err)
		}
		items = append(items, dryRunMigration{
			Version:  f.Version,
			Filename: f.Filename,
			SQL:      string(sqlBytes),
		})
	}

	if jsonOutput {
		b, err := json.Marshal(dryRunResult{
			Target:  targetName,
			DryRun:  true,
			Pending: items,
		})
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}

	if len(items) == 0 {
		fmt.Printf("%s: already up to date, no pending migrations (dry-run)\n", targetName)
		return nil
	}

	fmt.Printf("%s: %d pending migration(s) (dry-run):\n\n", targetName, len(items))
	for _, item := range items {
		fmt.Printf("-- ===== %s (v%d) =====\n", item.Filename, item.Version)
		fmt.Println(item.SQL)
		fmt.Println()
	}
	return nil
}
