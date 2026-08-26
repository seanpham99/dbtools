package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/seanpham99/dbtools/internal/apply"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/logger"
	"github.com/seanpham99/dbtools/internal/statusinfo"
	"github.com/spf13/cobra"
)

var (
	pushYes    bool
	pushURL    string
	pushDryRun bool
)

var pushCmd = &cobra.Command{
	Use:   "push [target]",
	Short: "Apply pending migrations to a named remote target (version-sync only)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPush(args[0])
	},
}

func runPush(targetName string) (err error) {
	defer emitJobSummary(&err)
	cfg, err := loadConfig("dbtools.toml")
	if err != nil {
		return fmt.Errorf("loading dbtools.toml: %w", err)
	}
	if err := requireUnprotected(cfg, targetName); err != nil {
		return err
	}

	if pushDryRun {
		return runDryRun(cfg, targetName, pushURL)
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
	preview, err := statusinfo.Collect(url, cfg.MigrationsDir, cfg.Migrations.UpSuffix, targetName)
	if err != nil {
		return err
	}
	if len(preview.Pending) == 0 {
		if jsonOutput {
			b, err := json.Marshal(statusinfo.Status{
				Target:         targetName,
				CurrentVersion: preview.CurrentVersion,
				HasVersion:     preview.HasVersion,
				Dirty:          preview.Dirty,
				Pending:        nil,
			})
			if err != nil {
				return err
			}
			fmt.Println(string(b))
			return nil
		}
		logger.Infof("%s: already up to date, nothing to push", targetName)
		return nil
	}

	if !pushYes {
		if !jsonOutput {
			logger.Infof("%s: %d pending migration(s):", targetName, len(preview.Pending))
			for _, f := range preview.Pending {
				logger.Infof("  %s", f)
			}
		}
		return fmt.Errorf("refusing to push migrations to %q without --yes", targetName)
	}

	status, err := apply.Run(cfg, targetName, pushURL)
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
}

func init() {
	pushCmd.Flags().BoolVar(&pushYes, "yes", false, "confirm applying migrations to this target")
	pushCmd.Flags().StringVar(&pushURL, "url", "", "connection string override (overrides target's configured URL env var)")
	pushCmd.Flags().BoolVar(&pushDryRun, "dry-run", false, "print pending migration SQL without applying anything")
	rootCmd.AddCommand(pushCmd)
}
