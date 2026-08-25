package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/seanpham99/dbtools/internal/logger"
	"github.com/spf13/cobra"
)

var (
	upTarget string
	upURL    string
	upDryRun bool
)

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Apply pending migrations to the local target",
	RunE: func(cmd *cobra.Command, args []string) (err error) {
		defer emitJobSummary(&err)
		// up is the fast local dev loop. It deliberately refuses any
		// non-local target: reaching a remote database requires the
		// explicit `push <target> --yes` path with its preview + guard.
		if upTarget != "local" {
			return fmt.Errorf("refusing to run `up` against %q — use `push %s --yes` for a remote target (it previews pending migrations first)", upTarget, upTarget)
		}
		cfg, err := loadConfig("dbtools.toml")
		if err != nil {
			return fmt.Errorf("loading dbtools.toml: %w", err)
		}

		if upDryRun {
			return runDryRun(cfg, upTarget, upURL)
		}

		status, err := applyRun(cfg, upTarget, upURL)
		if err != nil {
			return err
		}

		if jsonOutput {
			b, err := json.Marshal(status)
			if err != nil {
				return err
			}
			fmt.Println(string(b))
			return nil
		}

		logger.Infof("%s: now at version %d (%d pending)", status.Target, status.CurrentVersion, len(status.Pending))
		return nil
	},
}

func init() {
	upCmd.Flags().StringVar(&upTarget, "target", "local", "target to apply migrations to")
	upCmd.Flags().StringVar(&upURL, "url", "", "connection string override (overrides target's configured URL env var)")
	upCmd.Flags().BoolVar(&upDryRun, "dry-run", false, "print pending migration SQL without applying anything")
	rootCmd.AddCommand(upCmd)
}
