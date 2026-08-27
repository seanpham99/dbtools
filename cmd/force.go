package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

var (
	forceTarget string
	forceYes    bool
)

var forceCmd = &cobra.Command{
	Use:   "force <version>",
	Short: "Force the migration tracking version and clear dirty state without running SQL",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		version, err := strconv.ParseUint(args[0], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid version %q: %w", args[0], err)
		}
		if !forceYes {
			return fmt.Errorf("force modifies migration version state directly without executing SQL — pass --yes to confirm")
		}
		return runForce(forceTarget, version)
	},
}

func init() {
	forceCmd.Flags().StringVar(&forceTarget, "target", "local", "target database to force version on")
	forceCmd.Flags().BoolVar(&forceYes, "yes", false, "confirm forcing migration version and clearing dirty state")
	rootCmd.AddCommand(forceCmd)
}

func runForce(targetName string, version uint64) error {
	cfg, err := loadConfig("dbtools.toml")
	if err != nil {
		return fmt.Errorf("loading dbtools.toml: %w", err)
	}
	if err := requireUnprotected(cfg, targetName); err != nil {
		return err
	}

	_, db, m, _, err := OpenTarget(cfg, targetName, "")
	if err != nil {
		return err
	}
	defer db.Close()

	// Force takes the migration lock and records the version as applied
	// without running its SQL — one write to one table, so there is no
	// longer a separate cursor to update alongside the ledger.
	if err := m.Force(context.Background(), version); err != nil {
		return err
	}

	if jsonOutput {
		b, err := json.Marshal(struct {
			Target  string `json:"target"`
			Version uint64 `json:"version"`
			Dirty   bool   `json:"dirty"`
		}{
			Target:  targetName,
			Version: version,
			Dirty:   false,
		})
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}

	fmt.Printf("%s: forced to version %d (dirty flag cleared)\n", targetName, version)
	return nil
}
