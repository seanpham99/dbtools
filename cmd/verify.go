package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/dbtools/dbtools/internal/config"
	"github.com/dbtools/dbtools/internal/engine"
	"github.com/dbtools/dbtools/internal/migrator"
	"github.com/dbtools/dbtools/internal/verify"
	"github.com/spf13/cobra"
)

var verifyCmd = &cobra.Command{
	Use:   "verify [target]",
	Short: "Check for drift between the migration ledger and the live database",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runVerify(args[0])
	},
}

var verifyInitLedger bool

func init() {
	verifyCmd.Flags().BoolVar(&verifyInitLedger, "init-ledger", false, "create the ledger table and backfill rows for already-applied migrations (default: verify is read-only and reports an uninitialised ledger)")
	rootCmd.AddCommand(verifyCmd)
}

func runVerify(targetName string) error {
	cfg, err := config.Load("dbtools.toml")
	if err != nil {
		return fmt.Errorf("loading dbtools.toml: %w", err)
	}
	url, err := cfg.ResolveURL(targetName)
	if err != nil {
		return err
	}

	eng, err := engine.ForTarget(cfg.EngineName(targetName), url)
	if err != nil {
		return err
	}

	db, err := eng.Open(url)
	if err != nil {
		return err
	}
	defer db.Close()

	m, err := migrator.Open(url, cfg.MigrationsDir)
	if err != nil {
		return err
	}
	defer m.Close()

	entries, err := eng.Ledger().List(db)
	if err != nil {
		// No ledger table at all: with --init-ledger, create it (and
		// backfill); without it, refuse — verify must not perform DDL/DML
		// on a target it was asked to inspect read-only.
		if verifyInitLedger {
			if err := eng.Ledger().Sync(db, m, cfg.MigrationsDir); err != nil {
				return err
			}
			entries, err = eng.Ledger().List(db)
			if err != nil {
				return err
			}
		} else {
			return fmt.Errorf("ledger for %q is not initialised — refusing to create it on a read-only check; pass --init-ledger to create and backfill it", targetName)
		}
	}
	if len(entries) == 0 {
		if !verifyInitLedger {
			return fmt.Errorf("ledger for %q is empty — refusing to create it on a read-only check; pass --init-ledger to create and backfill it", targetName)
		}
	}

	report, err := verify.Collect(db, eng, cfg.MigrationsDir, targetName)
	if err != nil {
		return err
	}

	if jsonOutput {
		b, err := json.Marshal(report.Entries)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
	} else {
		fmt.Printf("%-10s  %-16s  %-50s  %-6s  %s\n", "target", "version", "file", "status", "detail")
		for _, e := range report.Entries {
			fmt.Printf("%-10s  %-16d  %-50s  %-6s  %s\n", targetName, e.Version, e.File, e.Status, e.Detail)
		}
	}

	driftCount := 0
	for _, e := range report.Entries {
		if e.Status == "DRIFT" {
			driftCount++
		}
	}
	if driftCount > 0 {
		return fmt.Errorf("drift detected in %d migration(s)", driftCount)
	}
	return nil
}
