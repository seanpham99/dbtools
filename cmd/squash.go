package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/logger"
	"github.com/seanpham99/dbtools/internal/migrator"
	"github.com/seanpham99/dbtools/internal/scratchdb"
	"github.com/seanpham99/dbtools/internal/squash"
	"github.com/spf13/cobra"
)

var (
	squashUpto   uint64
	squashOut    string
	squashYes    bool
	squashDryRun bool
)

var squashCmd = &cobra.Command{
	Use:   "squash [target]",
	Short: "Collapse migration history into a verified baseline (dry-run by default)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSquash(args[0])
	},
}

func init() {
	squashCmd.Flags().Uint64Var(&squashUpto, "upto", 0, "highest version to collapse (default: highest version on disk)")
	squashCmd.Flags().StringVar(&squashOut, "out", "", "baseline filename override (default: 0000000000000_squashed_baseline.up.sql)")
	squashCmd.Flags().BoolVar(&squashYes, "yes", false, "write the baseline, archive collapsed files, and re-stamp the target (omit to only print the plan)")
	squashCmd.Flags().BoolVar(&squashDryRun, "dry-run", false, "explicit synonym for omitting --yes")
	rootCmd.AddCommand(squashCmd)
}

func runSquash(targetName string) error {
	cfg, err := loadConfig("dbtools.toml")
	if err != nil {
		return fmt.Errorf("loading dbtools.toml: %w", err)
	}
	url, err := cfg.ResolveURLOrFlag(targetName, "")
	if err != nil {
		return err
	}
	eng, err := engine.ForTarget(cfg.EngineName(targetName), url)
	if err != nil {
		return err
	}

	migrationsDir, upSuffix, _ := config.ResolveDefaults(cfg.MigrationsDir, cfg.Migrations.UpSuffix, cfg.Ledger.Table)
	dir, err := migrator.ReadDir(migrationsDir, upSuffix)
	if err != nil {
		return err
	}
	upto := squashUpto
	if upto == 0 {
		versions := dir.ListVersions()
		if len(versions) == 0 {
			return fmt.Errorf("no migration files found in %s", migrationsDir)
		}
		upto = versions[len(versions)-1]
	}

	// Pin the scratch databases to the target's server version. Best-effort:
	// if the target is unreachable we still build the plan, since squash
	// itself only writes files and the scratch databases are throwaway.
	targetSeries := ""
	if targetDB, derr := eng.Open(url); derr == nil {
		targetSeries = scratchdb.ServerSeries(targetDB, eng.Name())
		targetDB.Close()
	}

	plan, err := squash.BuildPlan(cfg, eng, migrationsDir, upSuffix, upto, targetSeries)
	if err != nil {
		return err
	}
	if !plan.Verified {
		return fmt.Errorf("baseline did not verify: %d structural difference(s) found — see findings", len(plan.Findings))
	}
	logger.Infof("baseline verified: collapsing versions %v", plan.CollapsedVersions)

	if !squashYes || squashDryRun {
		logger.Infof("dry run — pass --yes to write the baseline and archive %d file(s)", len(plan.CollapsedVersions))
		return nil
	}

	if err := requireUnprotected(cfg, targetName); err != nil {
		return err
	}

	baselineFilename := squashOut
	if baselineFilename == "" {
		baselineFilename = "0000000000000_squashed_baseline.up.sql"
	}
	archiveDir := filepath.Join(migrationsDir, "_archived")
	result, err := squash.ApplyPlan(cfg, targetName, eng, dir, migrationsDir, archiveDir, baselineFilename, plan)
	if err != nil {
		return err
	}
	logger.Infof("wrote %s, archived %d file(s), target state: %s", result.BaselineFile, len(result.ArchivedFiles), result.TargetState)
	return nil
}
