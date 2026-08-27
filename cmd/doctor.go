package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/ledger"
	"github.com/seanpham99/dbtools/internal/migrator"
	"github.com/seanpham99/dbtools/internal/scratchdb"
	"github.com/seanpham99/dbtools/internal/support"
	"github.com/seanpham99/dbtools/internal/verify"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor [target]",
	Short: "Perform read-only health and security checks on target databases",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Reset explicitly: cmd is a package-level singleton, so a prior
		// invocation's outcome would otherwise leak into every later one in
		// the same process. Same pattern as plan/verify/diff.
		cmd.SilenceUsage = false
		cmd.SilenceErrors = false
		target := ""
		if len(args) > 0 {
			target = args[0]
		}
		err := runDoctor(target)
		// Exit 1 (target unreachable) and exit 2 (issues detected) are both
		// documented outcomes of a correct run. doctor has already printed a
		// per-check report ending in "Result: ...", so the usage block and a
		// second rendering of the error add nothing. An actual
		// invalid-flag/argument error, which cobra routes through this same
		// path without an ExitCodeError, still shows both.
		var exitErr *ExitCodeError
		if errors.As(err, &exitErr) {
			cmd.SilenceUsage = true
			cmd.SilenceErrors = true
		}
		return err
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

// CheckResult represents the outcome of a single doctor check.
type CheckResult struct {
	Name    string `json:"name"`
	Status  string `json:"status"` // "ok", "warn", "fail"
	Message string `json:"message"`
}

// DoctorReport is the full doctor check report for one target.
type DoctorReport struct {
	Target  string        `json:"target"`
	Engine  string        `json:"engine"`
	Healthy bool          `json:"healthy"`
	Exit    int           `json:"exit"`
	Checks  []CheckResult `json:"checks"`
}

func evaluateTarget(cfg *config.Config, targetName string) *DoctorReport {
	report := &DoctorReport{
		Target: targetName,
		Checks: make([]CheckResult, 0, 6),
	}

	tConfig, hasTarget := cfg.Targets[targetName]
	if !hasTarget {
		report.Checks = append(report.Checks, CheckResult{
			Name:    "connectivity",
			Status:  "fail",
			Message: fmt.Sprintf("unknown target %q (known: %v)", targetName, cfg.TargetNames()),
		})
		report.Exit = 1
		report.Healthy = false
		return report
	}
	// Set engine early so human/JSON output has it even when a later
	// connectivity check fails before engine resolution.
	report.Engine = tConfig.Engine

	// 1. Security flags & config check
	secStatus := "ok"
	var secMessages []string
	if tConfig.URLEnv == "" {
		secStatus = "warn"
		secMessages = append(secMessages, "url_env not configured")
	} else {
		secMessages = append(secMessages, fmt.Sprintf("url_env=%s", tConfig.URLEnv))
	}
	secMessages = append(secMessages, fmt.Sprintf("protected=%v", tConfig.Protected))

	if _, err := os.Stat(cfg.MigrationsDir); os.IsNotExist(err) {
		secStatus = "warn"
		secMessages = append(secMessages, fmt.Sprintf("migrations_dir %q missing", cfg.MigrationsDir))
	}

	// 2. Connectivity check
	rawURL, err := cfg.ResolveURL(targetName)
	if err != nil {
		report.Checks = append(report.Checks, CheckResult{
			Name:    "connectivity",
			Status:  "fail",
			Message: fmt.Sprintf("resolving URL: %v", err),
		})
		report.Checks = append(report.Checks, CheckResult{
			Name:    "security-flags",
			Status:  secStatus,
			Message: strings.Join(secMessages, ", "),
		})
		report.Exit = 1
		report.Healthy = false
		return report
	}

	eng, err := engine.ForTarget(cfg.EngineName(targetName), rawURL)
	if err != nil {
		report.Checks = append(report.Checks, CheckResult{
			Name:    "connectivity",
			Status:  "fail",
			Message: fmt.Sprintf("engine resolution: %v", err),
		})
		report.Checks = append(report.Checks, CheckResult{
			Name:    "security-flags",
			Status:  secStatus,
			Message: strings.Join(secMessages, ", "),
		})
		report.Exit = 1
		report.Healthy = false
		return report
	}
	report.Engine = eng.Name()

	db, err := eng.Open(rawURL)
	if err != nil {
		report.Checks = append(report.Checks, CheckResult{
			Name:    "connectivity",
			Status:  "fail",
			Message: fmt.Sprintf("open failed: %v", err),
		})
		report.Checks = append(report.Checks, CheckResult{
			Name:    "security-flags",
			Status:  secStatus,
			Message: strings.Join(secMessages, ", "),
		})
		report.Exit = 1
		report.Healthy = false
		return report
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		report.Checks = append(report.Checks, CheckResult{
			Name:    "connectivity",
			Status:  "fail",
			Message: fmt.Sprintf("ping failed: %v", err),
		})
		report.Checks = append(report.Checks, CheckResult{
			Name:    "security-flags",
			Status:  secStatus,
			Message: strings.Join(secMessages, ", "),
		})
		report.Exit = 1
		report.Healthy = false
		return report
	}
	report.Checks = append(report.Checks, CheckResult{
		Name:    "connectivity",
		Status:  "ok",
		Message: fmt.Sprintf("connected to database (%s)", eng.Name()),
	})

	ledgerTableExists, err := engine.TableExists(eng, db, cfg.LedgerTableName())
	if err != nil {
		report.Healthy = false
		report.Exit = 1
		report.Checks = append(report.Checks, CheckResult{
			Name:    "ledger-integrity",
			Status:  "fail",
			Message: fmt.Sprintf("failed to check ledger table existence: %v", err),
		})
		return report
	}

	// 3. Ledger integrity check
	var ledgerEntries []ledger.Entry
	if !ledgerTableExists {
		report.Checks = append(report.Checks, CheckResult{
			Name:    "ledger-integrity",
			Status:  "skipped",
			Message: "no ledger — run `dbtools adopt` to enable",
		})
	} else {
		entries, err := eng.Ledger().List(db, cfg.LedgerTableName())
		if err != nil {
			report.Checks = append(report.Checks, CheckResult{
				Name:    "ledger-integrity",
				Status:  "fail",
				Message: fmt.Sprintf("reading ledger: %v", err),
			})
		} else {
			ledgerEntries = entries
			dir, dirErr := migrator.ReadDir(cfg.MigrationsDir, cfg.Migrations.UpSuffix)
			if dirErr != nil {
				report.Checks = append(report.Checks, CheckResult{
					Name:    "ledger-integrity",
					Status:  "fail",
					Message: fmt.Sprintf("reading migrations dir: %v", dirErr),
				})
			} else {
				hashMismatches := 0
				hashSkipped := 0
				hashVerified := 0
				for _, e := range entries {
					if e.Status != ledger.StatusApplied || e.ContentSHA256 == "" {
						continue
					}
					if e.HashSource == ledger.HashSourceAdopted {
						hashSkipped++
						continue
					}
					sum, err := dir.ContentHash(e.Version)
					if err != nil || sum != e.ContentSHA256 {
						hashMismatches++
						continue
					}
					hashVerified++
				}
				if hashMismatches > 0 {
					report.Checks = append(report.Checks, CheckResult{
						Name:    "ledger-integrity",
						Status:  "fail",
						Message: fmt.Sprintf("content hash mismatch in %d migration(s)", hashMismatches),
					})
				} else {
					msg := fmt.Sprintf("%d ledger entries verified (hashes match)", hashVerified)
					if hashSkipped > 0 {
						msg = fmt.Sprintf("%d ledger entries verified (hashes match), %d skipped (imported via adopt, unverified)", hashVerified, hashSkipped)
					}
					report.Checks = append(report.Checks, CheckResult{
						Name:    "ledger-integrity",
						Status:  "ok",
						Message: msg,
					})
				}
			}
		}
	}

	// Server version vs the tested window. Reported as its own check so a
	// surprising result elsewhere is attributable, rather than the user
	// discovering the mismatch only after distrusting a finding.
	{
		series := scratchdb.ServerSeries(db, eng.Name())
		if ok, msg := support.Check(eng.Name(), series); !ok {
			report.Checks = append(report.Checks, CheckResult{
				Name:    "server-version",
				Status:  "warn",
				Message: msg,
			})
		} else if series != "" {
			report.Checks = append(report.Checks, CheckResult{
				Name:    "server-version",
				Status:  "ok",
				Message: fmt.Sprintf("%s %s is within the tested range", eng.Name(), series),
			})
		}
	}

	// 4. Version sync & pending check
	var currentVer uint64
	var isDirty bool
	var hasVer bool
	if !ledgerTableExists {
		// Without a ledger dbtools has no record of what has been applied,
		// and there is no longer a separate cursor to fall back on. Saying
		// "N pending" would be a guess presented as a finding — a database
		// an incumbent tool already migrated looks identical to an empty
		// one from here. Skip, and point at the command that fixes it.
		report.Checks = append(report.Checks, CheckResult{
			Name:    "version-sync",
			Status:  "skipped",
			Message: "no ledger — run `dbtools adopt` to enable",
		})
	} else {
		var state ledger.State
		var vErr error
		state, vErr = eng.Ledger().State(db, cfg.LedgerTableName())
		v, dirty, hv := state.Version, state.Dirty, state.HasVersion
		if vErr != nil {
			report.Checks = append(report.Checks, CheckResult{
				Name:    "version-sync",
				Status:  "fail",
				Message: fmt.Sprintf("reading version: %v", vErr),
			})
		} else {
			currentVer = v
			isDirty = dirty
			hasVer = hv
			dir, dirErr := migrator.ReadDir(cfg.MigrationsDir, cfg.Migrations.UpSuffix)
			if dirErr != nil {
				report.Checks = append(report.Checks, CheckResult{
					Name:    "version-sync",
					Status:  "fail",
					Message: fmt.Sprintf("reading migrations dir: %v", dirErr),
				})
			} else {
				pending := dir.PendingFilenames(currentVer, hasVer)
				if len(pending) > 0 {
					report.Checks = append(report.Checks, CheckResult{
						Name:    "version-sync",
						Status:  "fail",
						Message: fmt.Sprintf("%d pending migration(s) (current version %d)", len(pending), currentVer),
					})
				} else if hasVer {
					msg := fmt.Sprintf("up to date (version %d)", currentVer)
					if !ledgerTableExists {
						msg += " (no dbtools ledger)"
					}
					report.Checks = append(report.Checks, CheckResult{
						Name:    "version-sync",
						Status:  "ok",
						Message: msg,
					})
				} else {
					report.Checks = append(report.Checks, CheckResult{
						Name:    "version-sync",
						Status:  "ok",
						Message: "no migrations applied (clean database)",
					})
				}
			}
		}
	}

	// 5. Drift summary
	if !ledgerTableExists {
		vReport, err := verify.Collect(db, eng, cfg.MigrationsDir, cfg.Migrations.UpSuffix, cfg.LedgerTableName(), targetName)
		if err != nil {
			report.Checks = append(report.Checks, CheckResult{
				Name:    "drift-summary",
				Status:  "fail",
				Message: fmt.Sprintf("drift check failed: %v", err),
			})
		} else {
			driftCount := 0
			for _, e := range vReport.Entries {
				if e.Status == "DRIFT" {
					driftCount++
				}
			}
			if driftCount > 0 {
				report.Checks = append(report.Checks, CheckResult{
					Name:    "drift-summary",
					Status:  "fail",
					Message: fmt.Sprintf("drift detected in %d migration(s) (no ledger — object presence only)", driftCount),
				})
			} else {
				report.Checks = append(report.Checks, CheckResult{
					Name:    "drift-summary",
					Status:  "ok",
					Message: "no schema drift detected (no ledger — object presence only)",
				})
			}
		}
	} else if len(ledgerEntries) > 0 {
		vReport, err := verify.Collect(db, eng, cfg.MigrationsDir, cfg.Migrations.UpSuffix, cfg.LedgerTableName(), targetName)
		if err != nil {
			report.Checks = append(report.Checks, CheckResult{
				Name:    "drift-summary",
				Status:  "fail",
				Message: fmt.Sprintf("drift check failed: %v", err),
			})
		} else {
			driftCount := 0
			for _, e := range vReport.Entries {
				if e.Status == "DRIFT" {
					driftCount++
				}
			}
			if driftCount > 0 {
				report.Checks = append(report.Checks, CheckResult{
					Name:    "drift-summary",
					Status:  "fail",
					Message: fmt.Sprintf("drift detected in %d migration(s)", driftCount),
				})
			} else {
				report.Checks = append(report.Checks, CheckResult{
					Name:    "drift-summary",
					Status:  "ok",
					Message: "no schema drift detected",
				})
			}
		}
	} else {
		report.Checks = append(report.Checks, CheckResult{
			Name:    "drift-summary",
			Status:  "warn",
			Message: "skipped (ledger empty)",
		})
	}

	// 6. Dirty ledger check
	if !ledgerTableExists && !hasVer {
		report.Checks = append(report.Checks, CheckResult{
			Name:    "dirty-ledger",
			Status:  "skipped",
			Message: "no ledger — run `dbtools adopt` to enable",
		})
	} else if isDirty {
		report.Checks = append(report.Checks, CheckResult{
			Name:    "dirty-ledger",
			Status:  "fail",
			Message: fmt.Sprintf("ledger is marked DIRTY at version %d (previous apply failed)", currentVer),
		})
	} else {
		report.Checks = append(report.Checks, CheckResult{
			Name:    "dirty-ledger",
			Status:  "ok",
			Message: "ledger clean (dirty=false)",
		})
	}

	// Add security-flags check
	report.Checks = append(report.Checks, CheckResult{
		Name:    "security-flags",
		Status:  secStatus,
		Message: strings.Join(secMessages, ", "),
	})

	// Determine overall exit code:
	// 0: all healthy (ok or warn)
	// 2: health issues found (any check fail)
	// 1: unreachable / invalid config (handled at entry)
	hasFail := false
	for _, c := range report.Checks {
		if c.Status == "fail" {
			hasFail = true
			break
		}
	}
	if hasFail {
		report.Exit = 2
		report.Healthy = false
	} else {
		report.Exit = 0
		report.Healthy = true
	}

	return report
}

func renderDoctorHuman(reports []*DoctorReport) string {
	var b strings.Builder
	for i, r := range reports {
		if i > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "Target: %s (%s)\n", r.Target, r.Engine)
		for _, c := range r.Checks {
			badge := "[OK]  "
			switch c.Status {
			case "warn":
				badge = "[WARN]"
			case "fail":
				badge = "[FAIL]"
			case "skipped":
				badge = "[SKIP]"
			}
			fmt.Fprintf(&b, "  %-6s  %-18s %s\n", badge, c.Name, c.Message)
		}
		verdict := "HEALTHY (exit 0)"
		if r.Exit == 1 {
			verdict = "ERROR (exit 1)"
		} else if r.Exit == 2 {
			verdict = "ISSUES DETECTED (exit 2)"
		}
		fmt.Fprintf(&b, "Result: %s\n", verdict)
	}
	return b.String()
}

func runDoctor(targetFilter string) error {
	cfg, err := loadConfig("dbtools.toml")
	if err != nil {
		return fmt.Errorf("loading dbtools.toml: %w", err)
	}

	var targetNames []string
	if targetFilter != "" {
		targetNames = []string{targetFilter}
	} else {
		targetNames = cfg.TargetNames()
	}

	if len(targetNames) == 0 {
		return fmt.Errorf("no targets configured in dbtools.toml")
	}

	reports := make([]*DoctorReport, 0, len(targetNames))
	maxExit := 0

	for _, name := range targetNames {
		rep := evaluateTarget(cfg, name)
		reports = append(reports, rep)
		if rep.Exit == 1 {
			maxExit = 1
		} else if rep.Exit == 2 && maxExit != 1 {
			maxExit = 2
		}
	}

	if jsonOutput {
		var b []byte
		if targetFilter != "" && len(reports) == 1 {
			b, err = json.Marshal(reports[0])
		} else {
			b, err = json.Marshal(reports)
		}
		if err != nil {
			return err
		}
		fmt.Println(string(b))
	} else {
		fmt.Print(renderDoctorHuman(reports))
	}

	if maxExit != 0 {
		msg := "health issues detected"
		if maxExit == 1 {
			msg = "target error or unreachable"
		}
		return ExitCode(maxExit, msg)
	}

	return nil
}
