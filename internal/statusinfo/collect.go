package statusinfo

import (
	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/ledger"
	"github.com/seanpham99/dbtools/internal/migrator"
)

// Status is the point-in-time migration state for one target.
type Status struct {
	Target         string   `json:"target"`
	CurrentVersion uint64   `json:"current_version"`
	HasVersion     bool     `json:"has_version"`
	Dirty          bool     `json:"dirty"`
	Pending        []string `json:"pending"`
	// Ignored lists migration files that sit below CurrentVersion but
	// were never applied — the state `up` cannot act on, because it only
	// steps forward from the watermark.
	//
	// Paths, not bare filenames: the file is a thing on disk the operator
	// has to find and renumber, and this is the value `up` prints when it
	// refuses to exit 0. Always non-nil ([] when there are none), per the
	// JSON contract in docs/exit-codes.md.
	Ignored []string `json:"ignored"`
}

// TargetResult is the outcome of collecting status for a single named target.
type TargetResult struct {
	Target       string
	Status       *Status
	Unconfigured bool
	Err          error
}

// Collect opens databaseURL, reads its current migration state from the
// ledger, and diffs it against every migration file in migrationsDir.
//
// Read-only: it never creates the ledger table. A database dbtools has
// never touched reports "no version" rather than being written to by a
// status query.
func Collect(databaseURL, engineName, migrationsDir, upSuffix, ledgerTable, targetName string) (*Status, error) {
	eng, err := engine.ForTarget(engineName, databaseURL)
	if err != nil {
		return nil, err
	}
	db, err := eng.Open(databaseURL)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Check for the table explicitly rather than treating any State error
	// as "no ledger". A missing table genuinely means nothing has been
	// applied; a dropped connection, a permission failure or a malformed
	// ledger do not, and reporting those as "no version, everything
	// pending" would hide the real fault behind a plausible answer.
	var state ledger.State
	// applied is the ledger's full applied set, needed (not just the
	// watermark) to tell an already-applied file below the watermark from
	// one that was never applied and never will be — see BelowWatermark.
	var applied []uint64
	exists, err := engine.TableExists(eng, db, ledgerTable)
	if err != nil {
		return nil, err
	}
	if exists {
		state, err = eng.Ledger().State(db, ledgerTable)
		if err != nil {
			return nil, err
		}
		applied, err = eng.Ledger().AppliedVersions(db, ledgerTable)
		if err != nil {
			return nil, err
		}
	}

	d, err := migrator.ReadDir(migrationsDir, upSuffix)
	if err != nil {
		return nil, err
	}

	ignored := []string{}
	for _, f := range d.BelowWatermark(state.Version, state.HasVersion, applied) {
		ignored = append(ignored, f.Path)
	}

	return &Status{
		Target:         targetName,
		CurrentVersion: state.Version,
		HasVersion:     state.HasVersion,
		Dirty:          state.Dirty,
		Pending:        d.PendingFilenames(state.Version, state.HasVersion),
		Ignored:        ignored,
	}, nil
}

// CollectAll gathers migration status for every configured target (or a single filtered target),
// returning a slice of TargetResult. Failures on one target never abort collection for others.
func CollectAll(cfg *config.Config, targetFilter, urlOverride string) []TargetResult {
	names := cfg.TargetNames()
	if targetFilter != "" {
		names = []string{targetFilter}
	}

	results := make([]TargetResult, 0, len(names))
	for _, name := range names {
		// With targetFilter, urlOverride applies to it. Without targetFilter, urlOverride is ignored.
		override := ""
		if targetFilter != "" {
			override = urlOverride
		}
		url, err := cfg.ResolveURLOrFlag(name, override)
		if err != nil {
			if targetFilter == "" && config.IsUnsetEnv(err) {
				results = append(results, TargetResult{Target: name, Unconfigured: true})
				continue
			}
			results = append(results, TargetResult{Target: name, Err: err})
			continue
		}
		if _, err := engine.ForTarget(cfg.EngineName(name), url); err != nil {
			results = append(results, TargetResult{Target: name, Err: err})
			continue
		}
		s, err := Collect(url, cfg.EngineName(name), cfg.MigrationsDir, cfg.Migrations.UpSuffix, cfg.LedgerTableName(), name)
		if err != nil {
			results = append(results, TargetResult{Target: name, Err: err})
			continue
		}
		results = append(results, TargetResult{Target: name, Status: s})
	}
	return results
}
