package statusinfo

import (
	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/migrator"
)

// Status is the point-in-time migration state for one target.
type Status struct {
	Target         string   `json:"target"`
	CurrentVersion uint64   `json:"current_version"`
	HasVersion     bool     `json:"has_version"`
	Dirty          bool     `json:"dirty"`
	Pending        []string `json:"pending"`
}

// TargetResult is the outcome of collecting status for a single named target.
type TargetResult struct {
	Target       string
	Status       *Status
	Unconfigured bool
	Err          error
}

// Collect opens databaseURL, reads its current migration version, and
// diffs it against every migration file in migrationsDir.
func Collect(databaseURL, migrationsDir, targetName string) (*Status, error) {
	m, err := migrator.Open(databaseURL, migrationsDir)
	if err != nil {
		return nil, err
	}
	defer m.Close()

	version, dirty, hasVersion, err := m.Version()
	if err != nil {
		return nil, err
	}

	d, err := migrator.ReadDir(migrationsDir)
	if err != nil {
		return nil, err
	}

	return &Status{
		Target:         targetName,
		CurrentVersion: version,
		HasVersion:     hasVersion,
		Dirty:          dirty,
		Pending:        d.PendingFilenames(version, hasVersion),
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
		s, err := Collect(url, cfg.MigrationsDir, name)
		if err != nil {
			results = append(results, TargetResult{Target: name, Err: err})
			continue
		}
		results = append(results, TargetResult{Target: name, Status: s})
	}
	return results
}
