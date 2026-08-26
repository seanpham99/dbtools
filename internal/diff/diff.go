package diff

import (
	"fmt"
	"os"

	"github.com/seanpham99/dbtools/internal/apply"
	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/container"
	"github.com/seanpham99/dbtools/internal/engine"
)

// Run provisions a scratch database (or uses againstURL if non-empty,
// skipping provisioning), replays every migration file into it via the
// real apply path, introspects both it and target, and returns the
// structural comparison. Never writes to target.
func Run(cfg *config.Config, targetName, againstURL string) ([]Finding, []string, error) {
	targetURL, err := cfg.ResolveURLOrFlag(targetName, "")
	if err != nil {
		return nil, nil, err
	}
	eng, err := engine.ForTarget(cfg.EngineName(targetName), targetURL)
	if err != nil {
		return nil, nil, err
	}

	scratchURL := againstURL
	var cleanup func() error
	if scratchURL == "" {
		if eng.Name() == "sqlite" {
			f, err := os.CreateTemp("", "dbtools-diff-scratch-*.db")
			if err != nil {
				return nil, nil, fmt.Errorf("creating scratch file: %w", err)
			}
			path := f.Name()
			f.Close()
			os.Remove(path) // sqlite creates it fresh on first open
			scratchURL = "sqlite://" + path
			cleanup = func() error { return os.Remove(path) }
		} else {
			url, c, err := container.StartScratch(eng.Name())
			if err != nil {
				return nil, nil, fmt.Errorf("provisioning scratch database: %w", err)
			}
			scratchURL = url
			cleanup = c
		}
	}
	if cleanup != nil {
		defer func() {
			_ = cleanup()
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

	targetDB, err := eng.Open(targetURL)
	if err != nil {
		return nil, nil, err
	}
	defer targetDB.Close()
	targetSchema, _, err := eng.Introspect(targetDB, cfg.Generate.Exclude)
	if err != nil {
		return nil, nil, fmt.Errorf("introspecting target database: %w", err)
	}

	findings, notes := Compare(scratchSchema, targetSchema)
	return findings, notes, nil
}
