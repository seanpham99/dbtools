package diff

import (
	"fmt"

	"github.com/seanpham99/dbtools/internal/apply"
	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/scratchdb"
)

// Run provisions a scratch database (or uses againstURL if non-empty,
// skipping provisioning), replays every migration file into it via the
// real apply path, introspects both it and target, and returns the
// structural comparison. Never writes to target.
func Run(cfg *config.Config, targetName, againstURL string) (findings []Finding, notes []string, err error) {
	targetURL, err := cfg.ResolveURLOrFlag(targetName, "")
	if err != nil {
		return nil, nil, err
	}
	eng, err := engine.ForTarget(cfg.EngineName(targetName), targetURL)
	if err != nil {
		return nil, nil, err
	}

	// Open the target first, purely to read its server major version: the
	// scratch database has to be the same major or the comparison reports
	// rendering differences between versions as schema drift. Best-effort —
	// an unknown version falls back to the default image.
	targetDB, err := eng.Open(targetURL)
	if err != nil {
		return nil, nil, err
	}
	defer targetDB.Close()
	targetMajor := scratchdb.ServerMajor(targetDB, eng.Name())

	scratchURL, cleanup, err := scratchdb.ProvisionMajor(eng, againstURL, targetMajor)
	if err != nil {
		return nil, nil, err
	}
	if cleanup != nil {
		defer func() {
			cerr := cleanup()
			if cerr == nil {
				return
			}
			if err != nil {
				// The run already failed — fold the cleanup failure into
				// that error rather than the notes slice, which callers
				// don't render on a non-nil error (see cmd/diff.go).
				err = fmt.Errorf("%w (scratch database cleanup also failed: %v)", err, cerr)
				return
			}
			notes = append(notes, fmt.Sprintf("warning: scratch database cleanup failed: %v", cerr))
		}()
	}

	if _, err := apply.Run(cfg, "diff-scratch", scratchURL); err != nil {
		return nil, nil, fmt.Errorf("replaying migrations into scratch database: %w", err)
	}

	scratchEng, err := engine.ForURL(scratchURL)
	if err != nil {
		return nil, nil, err
	}
	scratchDB, err := scratchEng.Open(scratchURL)
	if err != nil {
		return nil, nil, err
	}
	defer scratchDB.Close()
	scratchSchema, _, err := scratchEng.Introspect(scratchDB, cfg.Generate.Exclude)
	if err != nil {
		return nil, nil, fmt.Errorf("introspecting scratch database: %w", err)
	}

	targetSchema, _, err := eng.Introspect(targetDB, cfg.Generate.Exclude)
	if err != nil {
		return nil, nil, fmt.Errorf("introspecting target database: %w", err)
	}

	findings, notes = Compare(scratchSchema, targetSchema)
	return findings, notes, nil
}
