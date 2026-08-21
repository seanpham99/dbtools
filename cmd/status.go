package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/statusinfo"
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

// targetFailure is a target this run couldn't reach.
type targetFailure struct {
	Target string
	Error  string
}

// statusJSONEntry is the --json shape for one target: either the fields
// from statusinfo.Status, or just Target+Error when that target couldn't
// be reached.
type statusJSONEntry struct {
	Target         string   `json:"target"`
	CurrentVersion uint64   `json:"current_version,omitempty"`
	HasVersion     bool     `json:"has_version,omitempty"`
	Dirty          bool     `json:"dirty,omitempty"`
	Pending        []string `json:"pending,omitempty"`
	Error          string   `json:"error,omitempty"`
}

// buildStatusEntries merges successful and failed target results into one
// ordered slice for --json output.
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

func renderStatusTable(results []statusinfo.TargetResult) string {
	var b strings.Builder
	for _, r := range results {
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
	}
	return b.String()
}

func runStatus() error {
	cfg, err := config.Load("dbtools.toml")
	if err != nil {
		return fmt.Errorf("loading dbtools.toml: %w", err)
	}

	results := statusinfo.CollectAll(cfg, statusTarget, statusURL)

	if jsonOutput {
		var statuses []statusinfo.Status
		var failures []targetFailure
		for _, r := range results {
			if r.Err != nil {
				failures = append(failures, targetFailure{Target: r.Target, Error: r.Err.Error()})
			} else if r.Status != nil {
				statuses = append(statuses, *r.Status)
			}
		}
		b, err := json.Marshal(buildStatusEntries(statuses, failures))
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}

	fmt.Print(renderStatusTable(results))
	return nil
}
