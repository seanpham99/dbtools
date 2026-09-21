package cmd

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/seanpham99/dbtools/internal/logger"
	"github.com/seanpham99/dbtools/internal/statusinfo"
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
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = false
		err := runUp()
		var exitErr *ExitCodeError
		if errors.As(err, &exitErr) {
			cmd.SilenceUsage = true
		}
		return err
	},
}

func runUp() error {
	// up is the fast local dev loop. It deliberately refuses any
	// non-local target: reaching a remote database requires the
	// explicit `push <target> --yes` path with its preview + guard.
	if upTarget != "local" {
		return ExitCode(1, fmt.Sprintf("refusing to run `up` against %q — use `push %s --yes` for a remote target (it previews pending migrations first)", upTarget, upTarget))
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
		b, err := json.Marshal(struct {
			statusinfo.Status
			OK bool `json:"ok"`
		}{Status: *status, OK: true})
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return belowWatermarkRefusal(status)
	}

	logger.Infof("%s: now at version %d (%d pending)", status.Target, status.CurrentVersion, len(status.Pending))
	return belowWatermarkRefusal(status)
}

// belowWatermarkRefusal fails a run when the target holds migration files
// that sit below its current watermark and were never applied.
//
// Both `up` and `push` step forward from the watermark only, so those files
// are passed over on this run and on every later one. Exiting 0 with
// "0 pending" makes that indistinguishable from a correct, fully-applied
// target — the defect this guards against — so the run prints every ignored
// path and returns the documented exit 2 (drift/pending: inspection found
// something that needs a human). Nothing is repaired automatically:
// applying a migration out of order is not this tool's decision to make.
//
// The refusal is reported on stderr by the logger and the error carries an
// empty message, like the other exit-2 commands (`diff`, `verify`, `plan`):
// main.go prints ExitCodeError.Message as well as cobra printing it, so a
// non-empty message here would print the same paragraph twice.
func belowWatermarkRefusal(status *statusinfo.Status) error {
	if status == nil || len(status.Ignored) == 0 {
		return nil
	}
	logger.Errorf("%s: %d migration file(s) below the current watermark (v%d) were skipped and will never be applied:",
		status.Target, len(status.Ignored), status.CurrentVersion)
	for _, path := range status.Ignored {
		logger.Errorf("%s:   %s", status.Target, path)
	}
	logger.Errorf("%s: renumber them above v%d (`dbtools new`), or record the version you actually applied with `dbtools repair %s <version>:applied` — nothing is applied out of order",
		status.Target, status.CurrentVersion, status.Target)
	return ExitCode(2, "")
}

func init() {
	upCmd.Flags().StringVar(&upTarget, "target", "local", "target to apply migrations to")
	upCmd.Flags().StringVar(&upURL, "url", "", "connection string override (overrides target's configured URL env var)")
	upCmd.Flags().BoolVar(&upDryRun, "dry-run", false, "print pending migration SQL without applying anything")
	rootCmd.AddCommand(upCmd)
}
