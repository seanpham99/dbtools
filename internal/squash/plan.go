package squash

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/seanpham99/dbtools/internal/apply"
	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/diff"
	"github.com/seanpham99/dbtools/internal/dump"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/migrator"
	"github.com/seanpham99/dbtools/internal/scratchdb"
)

// Plan is the result of dumping and verifying a baseline, before any
// write happens.
type Plan struct {
	UptoVersion       uint64
	BaselineSQL       string
	Verified          bool
	Findings          []diff.Finding
	CollapsedVersions []uint64
}

// BuildPlan replays migrationsDir up to uptoVersion into a scratch
// database, dumps its schema, verifies the dump reproduces the same
// structure via a second scratch database and diff.Compare, and returns
// the result. Never writes to migrationsDir or to any named target —
// every database it touches is scratch/throwaway.
func BuildPlan(cfg *config.Config, eng engine.Engine, migrationsDir, upSuffix string, uptoVersion uint64) (*Plan, error) {
	dir, err := migrator.ReadDir(migrationsDir, upSuffix)
	if err != nil {
		return nil, err
	}
	var collapsed []uint64
	for _, v := range dir.ListVersions() {
		if v <= uptoVersion {
			collapsed = append(collapsed, v)
		}
	}
	if len(collapsed) == 0 {
		return nil, fmt.Errorf("no migration versions at or below %d found in %s", uptoVersion, migrationsDir)
	}

	url1, cleanup1, err := scratchdb.Provision(eng, "")
	if err != nil {
		return nil, fmt.Errorf("provisioning replay scratch database: %w", err)
	}
	if cleanup1 != nil {
		defer cleanup1()
	}

	replayMigrationsDir := migrationsDir
	if len(collapsed) < len(dir.ListVersions()) {
		tmpDir, err := os.MkdirTemp("", "dbtools-squash-replay-*")
		if err != nil {
			return nil, fmt.Errorf("creating temp replay dir: %w", err)
		}
		defer os.RemoveAll(tmpDir)
		for _, v := range collapsed {
			f, err := dir.Find(v)
			if err != nil {
				return nil, err
			}
			content, err := os.ReadFile(f.Path)
			if err != nil {
				return nil, fmt.Errorf("reading migration %s: %w", f.Filename, err)
			}
			if err := os.WriteFile(filepath.Join(tmpDir, f.Filename), content, 0o644); err != nil {
				return nil, fmt.Errorf("writing temp migration %s: %w", f.Filename, err)
			}
			if downF, err := dir.FindDown(v); err == nil {
				downContent, err := os.ReadFile(downF.Path)
				if err == nil {
					_ = os.WriteFile(filepath.Join(tmpDir, downF.Filename), downContent, 0o644)
				}
			}
		}
		replayMigrationsDir = tmpDir
	}

	replayCfg := *cfg
	replayCfg.MigrationsDir = replayMigrationsDir
	if _, err := apply.Run(&replayCfg, "squash-scratch-replay", url1); err != nil {
		return nil, fmt.Errorf("replaying migrations into scratch database: %w", err)
	}

	baselineSQL, err := dump.Schema(eng, url1)
	if err != nil {
		return nil, fmt.Errorf("dumping scratch database schema: %w", err)
	}

	url2, cleanup2, err := scratchdb.Provision(eng, "")
	if err != nil {
		return nil, fmt.Errorf("provisioning verification scratch database: %w", err)
	}
	if cleanup2 != nil {
		defer cleanup2()
	}
	verifyDB, err := eng.Open(url2)
	if err != nil {
		return nil, err
	}
	defer verifyDB.Close()
	if _, err := verifyDB.Exec(baselineSQL); err != nil {
		return nil, fmt.Errorf("applying baseline to verification database: %w", err)
	}

	replayDB, err := eng.Open(url1)
	if err != nil {
		return nil, err
	}
	defer replayDB.Close()
	replaySchema, _, err := eng.Introspect(replayDB, cfg.Generate.Exclude)
	if err != nil {
		return nil, fmt.Errorf("introspecting replay database: %w", err)
	}
	baselineSchema, _, err := eng.Introspect(verifyDB, cfg.Generate.Exclude)
	if err != nil {
		return nil, fmt.Errorf("introspecting verification database: %w", err)
	}

	findings, _ := diff.Compare(replaySchema, baselineSchema)
	return &Plan{
		UptoVersion:       uptoVersion,
		BaselineSQL:       baselineSQL,
		Verified:          len(findings) == 0,
		Findings:          findings,
		CollapsedVersions: collapsed,
	}, nil
}
