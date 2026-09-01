package squash

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

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

	_, upSuffix, ledgerTable := config.ResolveDefaults(cfg.MigrationsDir, cfg.Migrations.UpSuffix, cfg.LedgerTableName())

	upPat := regexp.MustCompile(`^(\d+)_.+` + regexp.QuoteMeta(upSuffix) + `$`)
	mMatches := upPat.FindStringSubmatch(baselineFilename)
	if mMatches == nil {
		return nil, fmt.Errorf("invalid baseline filename %q: must match migration filename pattern <version>_<name>%s", baselineFilename, upSuffix)
	}
	ver, parseErr := strconv.ParseUint(mMatches[1], 10, 64)
	if parseErr != nil || ver != 0 {
		return nil, fmt.Errorf("invalid baseline filename %q: baseline version must be 0", baselineFilename)
	}
	if !filepath.IsLocal(baselineFilename) || strings.ContainsAny(baselineFilename, `/\`) {
		// IsLocal alone still accepts "0_name/baseline.up.sql", which would
		// write below the migrations dir while ReadDir only indexes the root.
		return nil, fmt.Errorf("invalid baseline filename %q: must be a plain filename inside the migrations dir", baselineFilename)
	}

	baselinePath := filepath.Join(migrationsDir, baselineFilename)

	url, err := cfg.ResolveURLOrFlag(targetName, "")
	if err != nil {
		return nil, err
	}
	db, err := eng.Open(url)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	// Squash rewrites the ledger — marking collapsed versions reverted and
	// installing the baseline — while also moving files on disk. It holds
	// the migration lock across all of it: a concurrent `up` executing one
	// of those versions would otherwise have its row rewritten underneath
	// it, corrupting the single source of truth.
	runner := migrator.NewRunner(eng, db, dir, ledgerTable)
	releaseLock, err := runner.LockForWrite(context.Background())
	if err != nil {
		return nil, err
	}
	defer releaseLock()

	state0, err := runner.State(context.Background())
	if err != nil {
		return nil, err
	}
	if state0.Dirty {
		return nil, &ledger.DirtyError{Version: state0.Applying, Table: ledgerTable}
	}
	currentVer, hasVersion := state0.Version, state0.HasVersion

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
	// Exclusive create instead of stat-then-write: it refuses any existing
	// entry — including a dangling symlink, which os.Stat misses and
	// WriteFile would follow — and closes the replace-between-check-and-
	// write race.
	bf, err := os.OpenFile(baselinePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("baseline file %s already exists in %s", baselineFilename, migrationsDir)
		}
		return nil, fmt.Errorf("writing baseline file: %w", err)
	}
	if _, err := bf.WriteString(plan.BaselineSQL); err != nil {
		bf.Close()
		return nil, fmt.Errorf("writing baseline file: %w", err)
	}
	if err := bf.Close(); err != nil {
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
		// The collapsed versions are replaced by baseline version 0. With
		// one table there is no separate cursor to re-stamp alongside it:
		// recording the baseline row IS the re-stamp.
		for _, v := range plan.CollapsedVersions {
			if v == 0 {
				continue
			}
			if err := eng.Ledger().SetStatus(db, v, ledger.StatusReverted,
				"collapsed into the squashed baseline", ledgerTable); err != nil {
				return nil, err
			}
		}

		sum := sha256.Sum256([]byte(plan.BaselineSQL))
		hash := hex.EncodeToString(sum[:])
		note := fmt.Sprintf("squash baseline — verified structurally equivalent to versions %v", plan.CollapsedVersions)
		if err := eng.Ledger().SetStatusWithHash(db, 0, ledger.StatusApplied, note, hash, ledgerTable); err != nil {
			return nil, err
		}
	}

	return &Result{BaselineFile: baselineFilename, ArchivedFiles: archived, TargetState: state}, nil
}
