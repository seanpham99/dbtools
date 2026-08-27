package squash

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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
//
// targetSeries pins both scratch databases to the target's server version; pass "" to accept the engine's default image. It matters twice
// over: the dump tool's output has to be accepted by the target that will
// eventually replay the committed baseline, and the verification comparison
// has to be free of the cross-version rendering differences described in
// container.ScratchImageFor and scratchdb.ServerSeries.
//
// useHostTools forces the host's dump binary instead of running the engine's
// own inside the scratch container — the escape hatch for environments
// without Docker.
func BuildPlan(cfg *config.Config, eng engine.Engine, migrationsDir, upSuffix string, uptoVersion uint64, targetSeries string, useHostTools bool) (plan *Plan, err error) {
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

	replay, err := scratchdb.ProvisionSeries(eng, "", targetSeries)
	if err != nil {
		return nil, fmt.Errorf("provisioning replay scratch database: %w", err)
	}
	url1 := replay.URL
	if cleanup1 := replay.Cleanup; cleanup1 != nil {
		defer func() {
			if cerr := cleanup1(); cerr != nil && err == nil {
				err = fmt.Errorf("scratch database cleanup failed: %w", cerr)
			}
		}()
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

	_, _, ledgerTable := config.ResolveDefaults(cfg.MigrationsDir, cfg.Migrations.UpSuffix, cfg.Ledger.Table)
	excludeTables := []string{"schema_migrations", "dbtools_migration_history", ledgerTable}
	// Dump from inside the replay container when the image ships the tool,
	// so the dump tool's version always matches the server it dumps.
	baselineSQL, err := dump.Schema(eng, url1, dump.Options{
		ExecIn:       replay.Container,
		UseHostTools: useHostTools,
	}, excludeTables...)
	if err != nil {
		return nil, fmt.Errorf("dumping scratch database schema: %w", err)
	}

	verify, err := scratchdb.ProvisionSeries(eng, "", targetSeries)
	if err != nil {
		return nil, fmt.Errorf("provisioning verification scratch database: %w", err)
	}
	url2 := verify.URL
	if cleanup2 := verify.Cleanup; cleanup2 != nil {
		defer func() {
			if cerr := cleanup2(); cerr != nil && err == nil {
				err = fmt.Errorf("verification database cleanup failed: %w", cerr)
			}
		}()
	}
	verifyURL := url2
	if eng.Name() == "mysql" {
		verifyURL = ensureMultiStatements(verifyURL)
	}
	verifyDB, err := eng.Open(verifyURL)
	if err != nil {
		return nil, err
	}
	defer verifyDB.Close()
	if err := execBaseline(verifyDB, baselineSQL); err != nil {
		return nil, fmt.Errorf("applying baseline to verification database: %w", err)
	}

	replayDB, err := eng.Open(url1)
	if err != nil {
		return nil, err
	}
	defer replayDB.Close()
	introspectExcludes := append([]string{}, cfg.Generate.Exclude...)
	introspectExcludes = append(introspectExcludes, excludeTables...)
	replaySchema, _, err := eng.Introspect(replayDB, introspectExcludes)
	if err != nil {
		return nil, fmt.Errorf("introspecting replay database: %w", err)
	}
	baselineSchema, _, err := eng.Introspect(verifyDB, introspectExcludes)
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

var goSeparator = regexp.MustCompile(`(?im)^\s*GO\s*$`)

func ensureMultiStatements(rawURL string) string {
	if strings.Contains(rawURL, "multiStatements=") {
		return rawURL
	}
	sep := "?"
	if strings.Contains(rawURL, "?") {
		sep = "&"
	}
	return rawURL + sep + "multiStatements=true"
}

func execBaseline(db *sql.DB, baselineSQL string) error {
	for _, batch := range goSeparator.Split(baselineSQL, -1) {
		batch = strings.TrimSpace(batch)
		if batch == "" {
			continue
		}
		if _, err := db.Exec(batch); err != nil {
			return err
		}
	}
	return nil
}
