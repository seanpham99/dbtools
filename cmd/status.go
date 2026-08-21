package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/dbtools/dbtools/internal/config"
	"github.com/dbtools/dbtools/internal/engine"
	"github.com/dbtools/dbtools/internal/render"
	"github.com/dbtools/dbtools/internal/statusinfo"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show migration status for every configured target",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStatus()
	},
}

var statusTarget string
var statusURL string

func init() {
	statusCmd.Flags().StringVar(&statusURL, "url", "", "connection string override (with --target: applies only to that named target; without it, ignored — status still iterates all targets)")
	statusCmd.Flags().StringVar(&statusTarget, "target", "", "only show this target's status (otherwise every configured target)")
	rootCmd.AddCommand(statusCmd)
}

// targetFailure is a target this run couldn't reach — e.g. its env var
// isn't set, which is normal on any machine other than the one host
// (typically a whitelisted CI/deploy box) that has DBTOOLS_PROD_URL. A
// failure for one target must never prevent showing the others.
type targetFailure struct {
	Target string
	Error  string
}

// statusJSONEntry is the --json shape for one target: either the fields
// from statusinfo.Status, or just Target+Error when that target couldn't
// be reached. Kept local to this command (not added to statusinfo.Status
// itself) so that type stays a plain successful-snapshot value — the same
// separation used by the read-only TUI dashboard.
type statusJSONEntry struct {
	Target         string   `json:"target"`
	CurrentVersion uint64   `json:"current_version,omitempty"`
	HasVersion     bool     `json:"has_version,omitempty"`
	Dirty          bool     `json:"dirty,omitempty"`
	Pending        []string `json:"pending,omitempty"`
	Error          string   `json:"error,omitempty"`
}

// buildStatusEntries merges successful and failed target results into one
// ordered slice for --json output. Pure — no I/O — so it's unit-testable
// without a database.
func buildStatusEntries(statuses []statusinfo.Status, failures []targetFailure) []statusJSONEntry {
	entries := make([]statusJSONEntry, 0, len(statuses)+len(failures))
	for _, s := range statuses {
		entries = append(entries, statusJSONEntry{
			Target:         s.Target,
			CurrentVersion: s.CurrentVersion,
			HasVersion:     s.HasVersion,
			Dirty:          s.Dirty,
			Pending:        s.Pending,
		})
	}
	for _, f := range failures {
		entries = append(entries, statusJSONEntry{Target: f.Target, Error: f.Error})
	}
	return entries
}

// collectStatuses gathers each configured target's status, recording a
// failure (never aborting the whole run) for targets that can't be
// resolved, fail engine validation, or can't be reached. Engine
// resolution happens before any connection attempt, so a target whose
// configured engine contradicts its URL scheme is rejected without dialing.
func collectStatuses(cfg *config.Config) ([]statusinfo.Status, []targetFailure) {
	var statuses []statusinfo.Status
	var failures []targetFailure

	// With --target, only that one target is checked, and --url applies
	// to it alone. Without --target, --url is ignored (it would be wrong
	// to apply one override to every target).
	names := cfg.TargetNames()
	if statusTarget != "" {
		names = []string{statusTarget}
	}

	for _, name := range names {
		url, err := cfg.ResolveURLOrFlag(name, statusURL)
		if err != nil {
			failures = append(failures, targetFailure{Target: name, Error: err.Error()})
			continue
		}
		if _, err := engine.ForTarget(cfg.EngineName(name), url); err != nil {
			failures = append(failures, targetFailure{Target: name, Error: err.Error()})
			continue
		}
		s, err := statusinfo.Collect(url, cfg.MigrationsDir, name)
		if err != nil {
			failures = append(failures, targetFailure{Target: name, Error: err.Error()})
			continue
		}
		statuses = append(statuses, *s)
	}
	return statuses, failures
}

func runStatus() error {
	cfg, err := config.Load("dbtools.toml")
	if err != nil {
		return fmt.Errorf("loading dbtools.toml: %w", err)
	}

	statuses, failures := collectStatuses(cfg)

	if jsonOutput {
		b, err := json.Marshal(buildStatusEntries(statuses, failures))
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}

	fmt.Print(render.Table(statuses))
	for _, f := range failures {
		fmt.Printf("%-10s  error: %s\n", f.Target, f.Error)
	}
	return nil
}
