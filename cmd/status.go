package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/statusinfo"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status [target]",
	Short: "Show migration status for every configured target (or a single target)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// See plan's identical rationale for this explicit reset.
		cmd.SilenceUsage = false
		target := statusTarget
		if len(args) > 0 && args[0] != "" {
			target = args[0]
		}
		err := runStatus(target)
		// A target connection failure is a documented exit-1 outcome
		// (see #55), not a usage mistake — same rationale as plan/verify.
		var exitErr *ExitCodeError
		if errors.As(err, &exitErr) {
			cmd.SilenceUsage = true
		}
		return err
	},
}

var statusTarget string
var statusURL string

func init() {
	statusCmd.Flags().StringVar(&statusURL, "url", "", "connection string override (with --target: applies only to that named target; without it, ignored — status still iterates all targets)")
	statusCmd.Flags().StringVar(&statusTarget, "target", "", "only show this target's status (otherwise every configured target)")
	rootCmd.AddCommand(statusCmd)
}

// targetFailure is a target this run couldn't reach.
type targetFailure struct {
	Target string
	Error  string
}

// statusJSONEntry is the --json shape for one target.
type statusJSONEntry struct {
	Target         string   `json:"target"`
	CurrentVersion uint64   `json:"current_version"`
	HasVersion     bool     `json:"has_version,omitempty"`
	Dirty          bool     `json:"dirty,omitempty"`
	Pending        []string `json:"pending,omitempty"`
	NoLedger       bool     `json:"no_ledger,omitempty"`
	Unconfigured   bool     `json:"unconfigured,omitempty"`
	Error          string   `json:"error,omitempty"`
}

func collectStatuses(cfg *config.Config) ([]statusinfo.Status, []targetFailure) {
	results := statusinfo.CollectAll(cfg, statusTarget, statusURL)
	var statuses []statusinfo.Status
	var failures []targetFailure
	for _, r := range results {
		if r.Err != nil {
			failures = append(failures, targetFailure{Target: r.Target, Error: r.Err.Error()})
		} else if r.Status != nil {
			statuses = append(statuses, *r.Status)
		}
	}
	return statuses, failures
}

func renderStatusTable(results []statusinfo.TargetResult, noLedgerMap ...map[string]bool) string {
	var noLedgers map[string]bool
	if len(noLedgerMap) > 0 {
		noLedgers = noLedgerMap[0]
	}
	var b strings.Builder
	for _, r := range results {
		if r.Unconfigured {
			fmt.Fprintf(&b, "%-10s  [unconfigured]\n", r.Target)
			continue
		}
		if r.Err != nil {
			fmt.Fprintf(&b, "%-10s  error: %s\n", r.Target, r.Err.Error())
			continue
		}
		s := r.Status
		state := "up to date"
		if n := len(s.Pending); n > 0 {
			state = fmt.Sprintf("%d pending", n)
		}
		dirtyMark := ""
		if s.Dirty {
			dirtyMark = " [DIRTY]"
		}
		fmt.Fprintf(&b, "%-10s  %s%s\n", s.Target, state, dirtyMark)
		if noLedgers != nil && noLedgers[r.Target] {
			fmt.Fprintf(&b, "%-10s  no dbtools ledger — run `dbtools adopt` to enable drift tracking\n", "")
		}
	}
	return b.String()
}

func runStatus(targetNames ...string) error {
	target := statusTarget
	if len(targetNames) > 0 && targetNames[0] != "" {
		target = targetNames[0]
	}
	cfg, err := loadConfig("dbtools.toml")
	if err != nil {
		return fmt.Errorf("loading dbtools.toml: %w", err)
	}

	results := statusinfo.CollectAll(cfg, target, statusURL)

	var connErr error
	for _, r := range results {
		if r.Err != nil {
			connErr = r.Err
			break
		}
	}

	noLedgers := make(map[string]bool)
	for _, r := range results {
		// Checked regardless of whether a version was reported. With the
		// cursor gone, "no ledger" is exactly why there is no version — so
		// gating this on HasVersion would hide it in the one case it
		// matters most.
		if r.Status != nil {
			override := ""
			if target != "" {
				override = statusURL
			}
			if url, uerr := cfg.ResolveURLOrFlag(r.Target, override); uerr == nil {
				if eng, eerr := engine.ForTarget(cfg.EngineName(r.Target), url); eerr == nil {
					if db, derr := eng.Open(url); derr == nil {
						exists, existsErr := engine.TableExists(eng, db, cfg.LedgerTableName())
						if existsErr == nil && !exists {
							noLedgers[r.Target] = true
						}
						db.Close()
					}
				}
			}
		}
	}

	if jsonOutput {
		entries := make([]statusJSONEntry, 0, len(results))
		for _, r := range results {
			if r.Unconfigured {
				entries = append(entries, statusJSONEntry{
					Target:       r.Target,
					Unconfigured: true,
				})
			} else if r.Err != nil {
				entries = append(entries, statusJSONEntry{
					Target: r.Target,
					Error:  r.Err.Error(),
				})
			} else if r.Status != nil {
				entries = append(entries, statusJSONEntry{
					Target:         r.Status.Target,
					CurrentVersion: r.Status.CurrentVersion,
					HasVersion:     r.Status.HasVersion,
					Dirty:          r.Status.Dirty,
					Pending:        r.Status.Pending,
					NoLedger:       noLedgers[r.Target],
				})
			}
		}
		b, err := json.Marshal(entries)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		if connErr != nil {
			return ExitCode(1, "")
		}
		return nil
	}

	fmt.Print(renderStatusTable(results, noLedgers))
	if connErr != nil {
		return ExitCode(1, "")
	}
	return nil
}
