package cmd

import (
	"fmt"

	"github.com/seanpham99/dbtools/internal/apply"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/statusinfo"
	"github.com/spf13/cobra"
)

var pushYes bool
var pushURL string

var pushCmd = &cobra.Command{
	Use:   "push [target]",
	Short: "Apply pending migrations to a named remote target (version-sync only)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPush(args[0])
	},
}

func runPush(targetName string) error {
	cfg, err := loadConfig("dbtools.toml")
	if err != nil {
		return fmt.Errorf("loading dbtools.toml: %w", err)
	}
	if err := requireUnprotected(cfg, targetName); err != nil {
		return err
	}

	url, err := cfg.ResolveURLOrFlag(targetName, pushURL)
	if err != nil {
		return err
	}
	// Validate the target's engine against the URL scheme before the
	// status preflight opens any connection.
	if _, err := engine.ForTarget(cfg.EngineName(targetName), url); err != nil {
		return err
	}
	preview, err := statusinfo.Collect(url, cfg.MigrationsDir, targetName)
	if err != nil {
		return err
	}
	if len(preview.Pending) == 0 {
		fmt.Printf("%s: already up to date, nothing to push\n", targetName)
		return nil
	}
	fmt.Printf("%s: %d pending migration(s):\n", targetName, len(preview.Pending))
	for _, f := range preview.Pending {
		fmt.Printf("  %s\n", f)
	}
	if !pushYes {
		return fmt.Errorf("refusing to push migrations to %q without --yes", targetName)
	}

	status, err := apply.Run(cfg, targetName, pushURL)
	if err != nil {
		return err
	}
	fmt.Printf("%s: now at version %d (%d pending)\n", status.Target, status.CurrentVersion, len(status.Pending))
	return nil
}

func init() {
	pushCmd.Flags().BoolVar(&pushYes, "yes", false, "confirm applying migrations to this target")
	pushCmd.Flags().StringVar(&pushURL, "url", "", "connection string override (overrides target's configured URL env var)")
	rootCmd.AddCommand(pushCmd)
}
