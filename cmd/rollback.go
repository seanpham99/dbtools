package cmd

import (
	"fmt"
	"strconv"

	"github.com/seanpham99/dbtools/internal/rollback"
	"github.com/spf13/cobra"
)

var (
	rollbackYes bool
	rollbackURL string
)

var rollbackCmd = &cobra.Command{
	Use:   "rollback <target> [N]",
	Short: "Soft-revert the last N applied migrations in the ledger (metadata-only, safe for production)",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		targetName := args[0]
		steps := 1
		if len(args) == 2 {
			n, err := strconv.Atoi(args[1])
			if err != nil || n <= 0 {
				return fmt.Errorf("invalid step count %q: must be a positive integer", args[1])
			}
			steps = n
		}
		return runRollback(targetName, steps)
	},
}

func init() {
	rollbackCmd.Flags().BoolVarP(&rollbackYes, "yes", "y", false, "confirm soft-reverting migrations in the ledger")
	rollbackCmd.Flags().StringVar(&rollbackURL, "url", "", "connection string override")
	rootCmd.AddCommand(rollbackCmd)
}

func runRollback(targetName string, steps int) error {
	cfg, err := loadConfig("dbtools.toml")
	if err != nil {
		return fmt.Errorf("loading dbtools.toml: %w", err)
	}

	target, ok := cfg.Targets[targetName]
	if !ok {
		return fmt.Errorf("unknown target %q", targetName)
	}

	if target.Protected && !rollbackYes {
		return fmt.Errorf("target %q is protected — refusing to soft-revert ledger status without --yes", targetName)
	}

	res, err := rollback.Run(cfg, targetName, steps, rollbackURL)
	if err != nil {
		return err
	}

	if len(res.RevertedVersions) == 0 {
		fmt.Printf("%s: no applied migrations to soft-revert\n", targetName)
		return nil
	}

	fmt.Printf("%s: soft-reverted %d migration(s) in ledger (no tables dropped)\n", targetName, len(res.RevertedVersions))
	for _, v := range res.RevertedVersions {
		fmt.Printf("  marked v%d reverted\n", v)
	}
	if res.HasCursor {
		fmt.Printf("%s: cursor recomputed to version %d\n", targetName, res.NewCursor)
	} else {
		fmt.Printf("%s: no applied migrations remaining\n", targetName)
	}
	return nil
}
