package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/seanpham99/dbtools/internal/down"
	"github.com/seanpham99/dbtools/internal/logger"
	"github.com/seanpham99/dbtools/internal/migrator"
	"github.com/spf13/cobra"
)

var (
	downYes     bool
	downPreview bool
	downURL     string
)

var downCmd = &cobra.Command{
	Use:   "down <target> [N]",
	Short: "Revert the last N applied migrations (default 1) using .down.sql files",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = false
		targetName := args[0]
		steps := 1
		if len(args) == 2 {
			n, err := strconv.Atoi(args[1])
			if err != nil || n <= 0 {
				return fmt.Errorf("invalid step count %q: must be a positive integer", args[1])
			}
			steps = n
		}
		err := runDown(targetName, steps)
		var exitErr *ExitCodeError
		if errors.As(err, &exitErr) {
			cmd.SilenceUsage = true
		}
		return err
	},
}

func init() {
	downCmd.Flags().BoolVarP(&downYes, "yes", "y", false, "confirm applying down migrations")
	downCmd.Flags().BoolVar(&downPreview, "preview", false, "preview down migrations that would be executed without applying them")
	downCmd.Flags().StringVar(&downURL, "url", "", "connection string override")
	rootCmd.AddCommand(downCmd)
}

type downPreviewJSON struct {
	Target         string          `json:"target"`
	Preview        bool            `json:"preview"`
	DownMigrations []migrator.File `json:"down_migrations"`
}

func runDown(targetName string, steps int) error {
	cfg, err := loadConfig("dbtools.toml")
	if err != nil {
		return fmt.Errorf("loading dbtools.toml: %w", err)
	}

	target, ok := cfg.Targets[targetName]
	if !ok {
		return fmt.Errorf("unknown target %q", targetName)
	}

	plan, err := down.Preview(cfg, targetName, steps, downURL)
	if err != nil {
		return err
	}

	if downPreview {
		if jsonOutput {
			b, err := json.Marshal(downPreviewJSON{
				Target:         targetName,
				Preview:        true,
				DownMigrations: plan,
			})
			if err != nil {
				return err
			}
			fmt.Println(string(b))
			return nil
		}
		if len(plan) == 0 {
			logger.Infof("%s: no applied migrations to revert (preview)", targetName)
			return nil
		}
		logger.Infof("%s: %d down migration(s) would be executed:", targetName, len(plan))
		for _, f := range plan {
			logger.Infof("  %s", f.Filename)
		}
		return nil
	}

	if len(plan) == 0 {
		if jsonOutput {
			b, err := json.Marshal(down.Result{
				Target:           targetName,
				RevertedVersions: nil,
				CurrentVersion:   0,
				HasVersion:       false,
			})
			if err != nil {
				return err
			}
			fmt.Println(string(b))
			return nil
		}
		logger.Infof("%s: no applied migrations to revert", targetName)
		return nil
	}

	if target.Protected {
		if !jsonOutput {
			logger.Infof("%s: [PROTECTED TARGET] %d down migration(s) will be executed:", targetName, len(plan))
			for _, f := range plan {
				logger.Infof("  %s", f.Filename)
			}
		}
		if !downYes {
			return ExitCode(1, fmt.Sprintf("target %q is protected — refusing to run destructive down migrations without --yes", targetName))
		}
	}

	res, err := down.Run(cfg, targetName, steps, downURL)
	if err != nil {
		return err
	}

	if jsonOutput {
		b, err := json.Marshal(res)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}

	logger.Infof("%s: reverted %d migration(s)", targetName, len(res.RevertedVersions))
	for _, v := range res.RevertedVersions {
		logger.Infof("  reverted v%d", v)
	}
	if res.HasVersion {
		logger.Infof("%s: now at version %d", targetName, res.CurrentVersion)
	} else {
		logger.Infof("%s: no migrations remaining (version 0)", targetName)
	}
	return nil
}
