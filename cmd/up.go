package cmd

import (
	"fmt"

	"github.com/dbtools/dbtools/internal/apply"
	"github.com/dbtools/dbtools/internal/config"
	"github.com/spf13/cobra"
)

var upTarget string
var upURL string

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Apply pending migrations to a target (defaults to 'local')",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load("dbtools.toml")
		if err != nil {
			return fmt.Errorf("loading dbtools.toml: %w", err)
		}
		status, err := apply.Run(cfg, upTarget, upURL)
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
