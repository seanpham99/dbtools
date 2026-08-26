package cmd

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/migrator"
	"github.com/seanpham99/dbtools/internal/verify"
	"github.com/spf13/cobra"
)

var verifyCmd = &cobra.Command{
	Use:   "verify [target]",
	Short: "Check for drift between the migration ledger and the live database",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// See plan's identical rationale for this explicit reset.
		cmd.SilenceUsage = false
		err := runVerify(args[0])
		// See plan's identical rationale: only silence usage for the
		// documented exit-2 outcome, not for a real invalid-argument error.
		var exitErr *ExitCodeError
		if errors.As(err, &exitErr) {
			cmd.SilenceUsage = true
		}
		return err
	},
}

var verifyInitLedger bool

func init() {
	verifyCmd.Flags().BoolVar(&verifyInitLedger, "init-ledger", false, "create the ledger table and backfill rows for already-applied migrations (default: verify is read-only and reports an uninitialised ledger)")
	rootCmd.AddCommand(verifyCmd)
}

func runVerify(targetName string) error {
	cfg, err := loadConfig("dbtools.toml")
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

	ledgerExists, err := engine.TableExists(eng, db, cfg.Ledger.Table)
	if err != nil {
		return err
	}
	if !ledgerExists {
		version, dirty, _, err := m.Version()
		if err != nil {
			return err
		}
		if dirty {
			return fmt.Errorf("target %q: migration cursor is dirty at version %d; run `dbtools repair %s` to resolve it", targetName, version, targetName)
		}
	}
	if !ledgerExists && verifyInitLedger {
		if err := requireUnprotected(cfg, targetName); err != nil {
			return err
		}
		if err := eng.Ledger().Sync(db, m, cfg.MigrationsDir, cfg.Migrations.UpSuffix, cfg.Ledger.Table); err != nil {
			return err
		}
		ledgerExists = true
	}
	if ledgerExists {
		entries, err := eng.Ledger().List(db, cfg.Ledger.Table)
		if err != nil {
			return err
		}
		if len(entries) == 0 && !verifyInitLedger {
			return fmt.Errorf("ledger for %q is empty — refusing to create it on a read-only check; pass --init-ledger to create and backfill it", targetName)
		}
		if len(entries) == 0 {
			if err := requireUnprotected(cfg, targetName); err != nil {
				return err
			}
			if err := eng.Ledger().Sync(db, m, cfg.MigrationsDir, cfg.Migrations.UpSuffix, cfg.Ledger.Table); err != nil {
				return err
			}
		}
	}

	report, err := verify.Collect(db, eng, cfg.MigrationsDir, cfg.Migrations.UpSuffix, cfg.Ledger.Table, targetName)
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
		return ExitCode(2, fmt.Sprintf("drift detected in %d migration(s)", driftCount))
	}
	return nil
}
