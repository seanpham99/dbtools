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
	"github.com/seanpham99/dbtools/internal/statusinfo"
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
	// Ignored names migration files that sit below the target's watermark
	// and have no applied ledger row: this command can never apply them, so
	// a dry run that stayed silent about them would read as "nothing to do"
	// on a target that is not in the state its directory describes.
	// Never nil: the JSON contract emits `[]`, not `null`.
	Ignored []string `json:"ignored"`
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

	// PendingAfter only looks forward, so the files this target passed over
	// are invisible to it. Only the ledger's applied set can tell a file
	// that was never applied from one that is simply history; reading it
	// needs the ledger table, which exists exactly when HasVersion is set.
	ignoredPaths := []string{}
	if hasVer {
		applied, err := eng.Ledger().AppliedVersions(db, ledgerTable)
		if err != nil {
			return err
		}
		for _, f := range dir.BelowWatermark(curVer, hasVer, applied) {
			ignoredPaths = append(ignoredPaths, f.Path)
		}
	}

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
			Ignored: ignoredPaths,
		})
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return belowWatermarkDryRunRefusal(targetName, curVer, ignoredPaths)
	}

	if len(items) == 0 {
		if len(ignoredPaths) == 0 {
			fmt.Printf("%s: already up to date, no pending migrations (dry-run)\n", targetName)
		}
		return belowWatermarkDryRunRefusal(targetName, curVer, ignoredPaths)
	}

	fmt.Printf("%s: %d pending migration(s) (dry-run):\n\n", targetName, len(items))
	for _, item := range items {
		fmt.Printf("-- ===== %s (v%d) =====\n", item.Filename, item.Version)
		fmt.Println(item.SQL)
		fmt.Println()
	}
	return belowWatermarkDryRunRefusal(targetName, curVer, ignoredPaths)
}

// belowWatermarkDryRunRefusal routes the dry-run paths through the same
// refusal reporter `up`/`push` use, so one state cannot be described two
// different ways. A dry run has no statusinfo.Status of its own, so it is
// constructed from the watermark and the ignored paths.
func belowWatermarkDryRunRefusal(target string, watermark uint64, ignored []string) error {
	if len(ignored) == 0 {
		return nil
	}
	return belowWatermarkRefusal(&statusinfo.Status{
		Target:         target,
		CurrentVersion: watermark,
		Ignored:        ignored,
	})
}
