package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var upTarget string
var upURL string

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Apply pending migrations to the local target",
	RunE: func(cmd *cobra.Command, args []string) error {
		// up is the fast local dev loop. It deliberately refuses any
		// non-local target: reaching a remote database requires the
		// explicit `push <target> --yes` path with its preview + guard
		// (the two commands share one apply.Run underneath, so there is
		// no separate code path to drift).
		if upTarget != "local" {
			return fmt.Errorf("refusing to run `up` against %q — use `push %s --yes` for a remote target (it previews pending migrations first)", upTarget, upTarget)
		}
		cfg, err := loadConfig("dbtools.toml")
		if err != nil {
			return fmt.Errorf("loading dbtools.toml: %w", err)
		}
		status, err := applyRun(cfg, upTarget, upURL)
		if err != nil {
			return err
		}
		fmt.Printf("%s: now at version %d (%d pending)\n", status.Target, status.CurrentVersion, len(status.Pending))
		return nil
	},
}

func init() {
	upCmd.Flags().StringVar(&upTarget, "target", "local", "target to apply migrations to")
	upCmd.Flags().StringVar(&upURL, "url", "", "connection string override (overrides target's configured URL env var)")
	rootCmd.AddCommand(upCmd)
}
