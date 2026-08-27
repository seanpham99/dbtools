package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/ledger"
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

	eng, err := engine.ForTarget(cfg.EngineName(targetName), url)
	if err != nil {
		return err
	}

	// Deliberately not OpenTarget: it calls engine.EnsureDatabase for
	// unprotected targets, so routing a preview through it could *create*
	// the database it was asked to describe. A dry run must not be the
	// thing that provisions anything.
	//
	// Opening url directly also keeps --url meaningful; OpenTarget would
	// re-resolve the configured target and silently preview a different
	// database than the one requested.
	db, err := eng.Open(url)
	if err != nil {
		return err
	}
	defer db.Close()

	migrationsDir, upSuffix, ledgerTable := config.ResolveDefaults(cfg.MigrationsDir, cfg.Migrations.UpSuffix, cfg.LedgerTableName())
	previewDir, err := migrator.ReadDir(migrationsDir, upSuffix)
	if err != nil {
		return err
	}
	state, err := migrator.NewRunner(eng, db, previewDir, ledgerTable).State(context.Background())
	if err != nil {
		return err
	}
	if state.Dirty {
		return &ledger.DirtyError{Version: state.Applying, Table: ledgerTable}
	}
	curVer, hasVer := state.Version, state.HasVersion

	dir := previewDir

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
