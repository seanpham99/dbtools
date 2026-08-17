package cmd

import (
	"fmt"

	"github.com/dbtools/dbtools/internal/apply"
	"github.com/dbtools/dbtools/internal/config"
	"github.com/dbtools/dbtools/internal/statusinfo"
	"github.com/spf13/cobra"
)

var pushYes bool
var pushURL string

var pushCmd = &cobra.Command{
	Use:   "push [target]",
	Short: "Apply pending migrations to a named remote target (version-sync only)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load("dbtools.toml")
		if err != nil {
			return fmt.Errorf("loading dbtools.toml: %w", err)
		}

		url, err := cfg.ResolveURLOrFlag(args[0], pushURL)
		if err != nil {
			return err
		}
		preview, err := statusinfo.Collect(url, cfg.MigrationsDir, args[0])
		if err != nil {
			return err
		}
		if len(preview.Pending) == 0 {
			fmt.Printf("%s: already up to date, nothing to push\n", args[0])
			return nil
		}
		fmt.Printf("%s: %d pending migration(s):\n", args[0], len(preview.Pending))
		for _, f := range preview.Pending {
			fmt.Printf("  %s\n", f)
		}
		if !pushYes {
			return fmt.Errorf("refusing to push migrations to %q without --yes", args[0])
		}

		status, err := apply.Run(cfg, args[0], pushURL)
		if err != nil {
			return err
		}
		fmt.Printf("%s: now at version %d (%d pending)\n", status.Target, status.CurrentVersion, len(status.Pending))
		return nil
	},
}

func init() {
	pushCmd.Flags().BoolVar(&pushYes, "yes", false, "confirm applying migrations to this target")
	pushCmd.Flags().StringVar(&pushURL, "url", "", "connection string override (overrides target's configured URL env var)")
	rootCmd.AddCommand(pushCmd)
}
