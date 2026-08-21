package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/statusinfo"
	"github.com/seanpham99/dbtools/internal/verify"
	"github.com/spf13/cobra"
)

// planCmd previews what `up`/`push` would do without executing anything:
// pending migration files, ledger dirtiness, and drift on already-applied
// versions. It is the agent/CI-facing "show me what happens next" surface —
// exit 0 means "safe to apply", non-zero means "investigate first".
var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Preview pending migrations and drift without applying anything (read-only)",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPlan()
	},
}

var planTarget string
var planURL string

func init() {
	planCmd.Flags().StringVar(&planURL, "url", "", "connection string override (with --target: applies only to that named target; without it, ignored)")
	planCmd.Flags().StringVar(&planTarget, "target", "", "only plan this target (otherwise every configured target)")
	rootCmd.AddCommand(planCmd)
}

// planJSONEntry is one target's plan row. Pending + dirty come from the
// same collectStatuses path status uses; drift (when the target is
// reachable and has a ledger) is a read-only verify pass. Errors are
// per-target, never fatal to the whole run.
type planJSONEntry struct {
	Target         string   `json:"target"`
	CurrentVersion uint64   `json:"current_version,omitempty"`
	HasVersion     bool     `json:"has_version,omitempty"`
	Dirty          bool     `json:"dirty,omitempty"`
	Pending        []string `json:"pending,omitempty"`
	Drift          []string `json:"drift,omitempty"`
	Error          string   `json:"error,omitempty"`
}

func runPlan() error {
	cfg, err := config.Load("dbtools.toml")
	if err != nil {
		return fmt.Errorf("loading dbtools.toml: %w", err)
	}

	entries := buildPlanEntries(cfg)

	b, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

// buildPlanEntries collects the plan for every configured target (or the
// single --target). Pure apart from DB reads, so it's unit-testable
// against a real sqlite file without capturing stdout.
func buildPlanEntries(cfg *config.Config) []planJSONEntry {
	names := cfg.TargetNames()
	if planTarget != "" {
		names = []string{planTarget}
	}

	entries := make([]planJSONEntry, 0, len(names))
	for _, name := range names {
		url, err := cfg.ResolveURLOrFlag(name, planURL)
		if err != nil {
			entries = append(entries, planJSONEntry{Target: name, Error: err.Error()})
			continue
		}
		eng, err := engine.ForTarget(cfg.EngineName(name), url)
		if err != nil {
			entries = append(entries, planJSONEntry{Target: name, Error: err.Error()})
			continue
		}
		s, err := statusinfo.Collect(url, cfg.MigrationsDir, name)
		if err != nil {
			entries = append(entries, planJSONEntry{Target: name, Error: err.Error()})
			continue
		}
		e := planJSONEntry{
			Target:         name,
			CurrentVersion: s.CurrentVersion,
			HasVersion:     s.HasVersion,
			Dirty:          s.Dirty,
			Pending:        s.Pending,
		}
		if s.HasVersion {
			e.Drift = planDrift(url, eng, cfg.MigrationsDir, name)
		}
		entries = append(entries, e)
	}
	return entries
}

// planDrift runs a read-only verify pass against url and returns the
// drift details for applied/reverted versions. Any verify failure is
// reported as a single "verify error" entry rather than aborting the
// plan — the plan's job is to surface problems, not to be blocked by
// them.
func planDrift(url string, eng engine.Engine, migrationsDir, targetName string) []string {
	db, err := eng.Open(url)
	if err != nil {
		return []string{"verify: " + err.Error()}
	}
	defer db.Close()

	report, err := verify.Collect(db, eng, migrationsDir, targetName)
	if err != nil {
		return []string{"verify: " + err.Error()}
	}
	var drift []string
	for _, e := range report.Entries {
		if e.Status != "OK" {
			drift = append(drift, fmt.Sprintf("v%d: %s", e.Version, e.Detail))
		}
	}
	return drift
}
