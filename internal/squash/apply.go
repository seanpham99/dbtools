package squash

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/ledger"
	"github.com/seanpham99/dbtools/internal/migrator"
)

// TargetState describes what ApplyPlan found <target>'s cursor to be,
// for the caller to report.
type TargetState string

const (
	TargetFresh            TargetState = "fresh"
	TargetRestamped        TargetState = "restamped"
	TargetPartiallyApplied TargetState = "partially_applied"
)

// Result summarizes what ApplyPlan wrote.
type Result struct {
	BaselineFile  string
	ArchivedFiles []string
	TargetState   TargetState
}

// ApplyPlan writes plan's baseline file, archives its collapsed files,
// and — if targetName's cursor is already at or past plan.UptoVersion —
// re-stamps it to the baseline version with a verified ledger record.
// Refuses (no writes at all) if plan isn't Verified, or if targetName's
// cursor sits strictly between 0 and plan.UptoVersion (ambiguous:
// partially through the history being collapsed).
func ApplyPlan(cfg *config.Config, targetName string, eng engine.Engine, dir *migrator.Dir, migrationsDir, archiveDir, baselineFilename string, plan *Plan) (*Result, error) {
	if !plan.Verified {
		return nil, fmt.Errorf("refusing to write: baseline did not verify (%d structural difference(s) found)", len(plan.Findings))
	}

	_, upSuffix, ledgerTable := config.ResolveDefaults(cfg.MigrationsDir, cfg.Migrations.UpSuffix, cfg.Ledger.Table)

	upPat := regexp.MustCompile(`^(\d+)_.+` + regexp.QuoteMeta(upSuffix) + `$`)
	mMatches := upPat.FindStringSubmatch(baselineFilename)
	if mMatches == nil {
		return nil, fmt.Errorf("invalid baseline filename %q: must match migration filename pattern <version>_<name>%s", baselineFilename, upSuffix)
	}
	ver, parseErr := strconv.ParseUint(mMatches[1], 10, 64)
	if parseErr != nil || ver != 0 {
		return nil, fmt.Errorf("invalid baseline filename %q: baseline version must be 0", baselineFilename)
	}

	baselinePath := filepath.Join(migrationsDir, baselineFilename)
	if _, statErr := os.Stat(baselinePath); statErr == nil {
		return nil, fmt.Errorf("baseline file %s already exists in %s", baselineFilename, migrationsDir)
	}

	url, err := cfg.ResolveURLOrFlag(targetName, "")
	if err != nil {
		return nil, err
	}
	m, err := migrator.Open(url, migrationsDir)
	if err != nil {
		return nil, err
	}
	defer m.Close()

	currentVer, dirty, hasVersion, err := m.Version()
	if err != nil {
		return nil, err
	}
	if dirty {
		return nil, fmt.Errorf("target %q: migration cursor is dirty at version %d; run `dbtools repair %s` first", targetName, currentVer, targetName)
	}

	var state TargetState
	switch {
	case !hasVersion:
		state = TargetFresh
	case currentVer >= plan.UptoVersion:
		state = TargetRestamped
	default:
		return nil, fmt.Errorf("target %q: cursor is at version %d, strictly between 0 and %d (%s) — finish applying or choose a smaller --upto", targetName, currentVer, plan.UptoVersion, TargetPartiallyApplied)
	}

	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating archive directory: %w", err)
	}
	if err := os.WriteFile(baselinePath, []byte(plan.BaselineSQL), 0o644); err != nil {
		return nil, fmt.Errorf("writing baseline file: %w", err)
	}

	var archived []string
	for _, v := range plan.CollapsedVersions {
		f, err := dir.Find(v)
		if err != nil {
			return nil, err
		}
		dest := filepath.Join(archiveDir, f.Filename)
		if err := os.Rename(f.Path, dest); err != nil {
			return nil, fmt.Errorf("archiving %s: %w", f.Filename, err)
		}
		archived = append(archived, f.Filename)
		if downFile, downErr := dir.FindDown(v); downErr == nil {
			downDest := filepath.Join(archiveDir, downFile.Filename)
			if err := os.Rename(downFile.Path, downDest); err != nil {
				return nil, fmt.Errorf("archiving %s: %w", downFile.Filename, err)
			}
			archived = append(archived, downFile.Filename)
		}
	}

	if state == TargetRestamped {
		if currentVer == plan.UptoVersion {
			if err := m.Stamp(0); err != nil {
				return nil, err
			}
		}
		db, err := eng.Open(url)
		if err != nil {
			return nil, err
		}
		defer db.Close()

		sum := sha256.Sum256([]byte(plan.BaselineSQL))
		hash := hex.EncodeToString(sum[:])
		note := fmt.Sprintf("squash baseline — verified structurally equivalent to versions %v", plan.CollapsedVersions)
		if err := eng.Ledger().SetStatusWithHash(db, 0, ledger.StatusApplied, note, hash, ledgerTable); err != nil {
			return nil, err
		}
	}

	return &Result{BaselineFile: baselineFilename, ArchivedFiles: archived, TargetState: state}, nil
}
