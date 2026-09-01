package cmd

import (
	"encoding/json"
	"errors"
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
		// Reset explicitly: cmd is a package-level singleton, so a prior
		// invocation's exit-2 outcome (which sets this true, below) would
		// otherwise leak into every later invocation in the same process
		// — including, notably, every other test in this package's suite.
		cmd.SilenceUsage = false
		err := runPlan()
		// Exit 2 (pending/drift) is a documented, expected outcome of a
		// correct run, not a usage mistake — only silence usage for that
		// specific case, so an actual invalid-flag/argument error (which
		// cobra also routes through this same error path) still shows it.
		var exitErr *ExitCodeError
		if errors.As(err, &exitErr) {
			cmd.SilenceUsage = true
		}
		return err
	},
}

var planTarget string
var planURL string

func init() {
	planCmd.Flags().StringVar(&planURL, "url", "", "connection string override (with --target: applies only to that named target; without it, ignored)")
	planCmd.Flags().StringVar(&planTarget, "target", "", "only plan this target (otherwise every configured target)")
	rootCmd.AddCommand(planCmd)
}

// planJSONEntry is one target's plan row.
type planJSONEntry struct {
	Target         string   `json:"target"`
	CurrentVersion uint64   `json:"current_version"`
	HasVersion     bool     `json:"has_version"`
	Dirty          bool     `json:"dirty"`
	Pending        []string `json:"pending"`
	Drift          []string `json:"drift"`
	LedgerSkipped  bool     `json:"ledger_skipped"`
	Error          string   `json:"error,omitempty"`
}

func runPlan() error {
	cfg, err := loadConfig("dbtools.toml")
	if err != nil {
		return fmt.Errorf("loading dbtools.toml: %w", err)
	}

	entries := buildPlanEntries(cfg)

	b, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	fmt.Println(string(b))

	var hasDriftOrPending bool
	var hasError bool
	for _, e := range entries {
		if e.Error != "" {
			hasError = true
		}
		if len(e.Pending) > 0 || len(e.Drift) > 0 || e.Dirty {
			hasDriftOrPending = true
		}
	}
	if hasError {
		return ExitCode(1, "")
	}
	if hasDriftOrPending {
		return ExitCode(2, "")
	}
	return nil
}

// buildPlanEntries collects the plan for every configured target (or the single --target).
func buildPlanEntries(cfg *config.Config) []planJSONEntry {
	results := statusinfo.CollectAll(cfg, planTarget, planURL)
	entries := make([]planJSONEntry, 0, len(results))

	for _, r := range results {
		if r.Err != nil {
			entries = append(entries, planJSONEntry{
				Target:  r.Target,
				Pending: []string{},
				Drift:   []string{},
				Error:   r.Err.Error(),
			})
			continue
		}
		s := r.Status
		pending := s.Pending
		if pending == nil {
			pending = []string{}
		}
		drift := []string{}
		var ledgerSkipped bool
		{
			override := ""
			if planTarget != "" {
				override = planURL
			}
			url, _ := cfg.ResolveURLOrFlag(r.Target, override)
			eng, err := engine.ForTarget(cfg.EngineName(r.Target), url)
			if err == nil {
				d, ls := planDrift(url, eng, cfg.MigrationsDir, cfg.Migrations.UpSuffix, cfg.LedgerTableName(), r.Target)
				if d != nil {
					drift = d
				}
				ledgerSkipped = ls
			}
		}
		e := planJSONEntry{
			Target:         r.Target,
			CurrentVersion: s.CurrentVersion,
			HasVersion:     s.HasVersion,
			Dirty:          s.Dirty,
			Pending:        pending,
			Drift:          drift,
			LedgerSkipped:  ledgerSkipped,
		}
		entries = append(entries, e)
	}
	return entries
}

// planDrift runs a read-only verify pass against url and returns the
// drift details for applied/reverted versions, plus whether the ledger
// table doesn't exist (in which case Collect walked files directly and
// its OK-status entries are not genuine drift).
func planDrift(url string, eng engine.Engine, migrationsDir, upSuffix, table, targetName string) ([]string, bool) {
	db, err := eng.Open(url)
	if err != nil {
		return []string{"verify: " + err.Error()}, false
	}
	defer db.Close()

	ledgerSkipped, err := engine.TableExists(eng, db, table)
	if err != nil {
		return []string{"verify: " + err.Error()}, false
	}
	ledgerSkipped = !ledgerSkipped

	report, err := verify.Collect(db, eng, migrationsDir, upSuffix, table, targetName)
	if err != nil {
		return []string{"verify: " + err.Error()}, ledgerSkipped
	}
	drift := []string{}
	for _, e := range report.Entries {
		if e.Status != "OK" {
			drift = append(drift, fmt.Sprintf("v%d: %s", e.Version, e.Detail))
		}
	}
	return drift, ledgerSkipped
}
