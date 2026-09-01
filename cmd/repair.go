package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/ledger"
	"github.com/seanpham99/dbtools/internal/logger"
	"github.com/seanpham99/dbtools/internal/repair"
	"github.com/spf13/cobra"
)

var (
	repairYes   bool
	repairForce bool
)

var repairCmd = &cobra.Command{
	Use:   "repair [target] [version:status[,version:status...]]",
	Short: "Correct the migration ledger's applied/reverted status for one or more versions",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = false
		if !repairYes {
			cmd.SilenceUsage = true
			return ExitCode(1, "repair does not execute any migration SQL — pass --yes to confirm you intend this")
		}
		pairs, err := parseRepairArgs(args[1])
		if err != nil {
			return err
		}
		err = runRepair(args[0], pairs)
		var exitErr *ExitCodeError
		if errors.As(err, &exitErr) {
			cmd.SilenceUsage = true
		}
		return err
	},
}

func init() {
	repairCmd.Flags().BoolVar(&repairYes, "yes", false, "confirm this repair is intentional")
	repairCmd.Flags().BoolVar(&repairForce, "force", false, "mark a version applied even if its migration's objects don't exist yet")
	rootCmd.AddCommand(repairCmd)
}

// parseRepairArgs parses "version:status,version:status,..." into pairs,
// rejecting invalid versions, invalid statuses, and duplicate versions.
func parseRepairArgs(arg string) ([]repair.Pair, error) {
	parts := strings.Split(arg, ",")
	pairs := make([]repair.Pair, 0, len(parts))
	seen := make(map[uint64]bool, len(parts))
	for _, p := range parts {
		kv := strings.SplitN(p, ":", 2)
		if len(kv) != 2 {
			return nil, fmt.Errorf("invalid version:status pair %q (want VERSION:applied or VERSION:reverted)", p)
		}
		version, err := strconv.ParseUint(kv[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid version %q in pair %q: %w", kv[0], p, err)
		}
		var status ledger.Status
		switch kv[1] {
		case "applied":
			status = ledger.StatusApplied
		case "reverted":
			status = ledger.StatusReverted
		default:
			return nil, fmt.Errorf("invalid status %q in pair %q (want applied or reverted)", kv[1], p)
		}
		if seen[version] {
			return nil, fmt.Errorf("duplicate version %d in repair args", version)
		}
		seen[version] = true
		pairs = append(pairs, repair.Pair{Version: version, Status: status})
	}
	return pairs, nil
}

func runRepair(targetName string, pairs []repair.Pair) error {
	cfg, err := loadConfig("dbtools.toml")
	if err != nil {
		return fmt.Errorf("loading dbtools.toml: %w", err)
	}
	if err := requireUnprotected(cfg, targetName); err != nil {
		return err
	}
	url, err := cfg.ResolveURL(targetName)
	if err != nil {
		return err
	}

	eng, err := engine.ForTarget(cfg.EngineName(targetName), url)
	if err != nil {
		return err
	}

	db, err := eng.Open(url)
	if err != nil {
		return err
	}
	defer db.Close()

	result, err := repair.Run(db, eng, cfg.MigrationsDir, cfg.Migrations.UpSuffix, cfg.LedgerTableName(), pairs, repairForce)
	if err != nil {
		return err
	}

	if jsonOutput {
		b, err := json.Marshal(result)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}

	if !result.HasVersion {
		logger.Infof("%s: repaired %d version(s), no applied versions remain", targetName, len(result.Repaired))
		return nil
	}
	logger.Infof("%s: repaired %d version(s), now at version %d", targetName, len(result.Repaired), result.NewVersion)
	return nil
}
