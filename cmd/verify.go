package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/dbtools/dbtools/internal/config"
	"github.com/dbtools/dbtools/internal/dbconn"
	"github.com/dbtools/dbtools/internal/ledger"
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

func init() {
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

	db, err := dbconn.Open(url)
	if err != nil {
		return err
	}
	defer db.Close()

	m, err := migrator.Open(url, cfg.MigrationsDir)
	if err != nil {
		return err
	}
	defer m.Close()

	if err := ledger.Sync(db, m, cfg.MigrationsDir); err != nil {
		return err
	}

	report, err := verify.Collect(db, cfg.MigrationsDir, targetName)
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
