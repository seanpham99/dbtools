package verify

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/ddlcheck"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/ledger"
	"github.com/seanpham99/dbtools/internal/migrator"
)

// Entry is one migration version's drift-check result.
type Entry struct {
	Version uint64
	File    string
	Status  string // "OK" or "DRIFT"
	Detail  string
}

// Report is the full drift report for one target.
type Report struct {
	Target  string
	Entries []Entry
}

func Collect(db *sql.DB, eng engine.Engine, migrationsDir, upSuffix, table, targetName string) (*Report, error) {
	migrationsDir, upSuffix, table = config.ResolveDefaults(migrationsDir, upSuffix, table)

	exists, err := engine.TableExists(eng, db, table)
	if err != nil {
		return nil, err
	}
	if !exists {
		return collectNoLedger(db, eng, migrationsDir, upSuffix, targetName)
	}

	entries, err := eng.Ledger().List(db, table)
	if err != nil {
		return nil, err
	}

	dir, err := migrator.ReadDir(migrationsDir, upSuffix)
	if err != nil {
		return nil, err
	}

	// A name is owned by the last migration that had something to say about
	// it — the one that created it, renamed onto it, or dropped it. A
	// version is excused for a name only when a strictly later version owns
	// it, because that later version is the one whose create or rename
	// failed to produce it. Naming an owner this way also keeps the
	// pre-existing case working: a version that DROPs an object another
	// version created owns that object's absence.
	lastWord := make(map[ddlcheck.ObjectRef]uint64) // object -> owning version
	for _, e := range entries {
		if e.Status != ledger.StatusApplied {
			continue
		}
		file, err := dir.Find(e.Version)
		if err != nil {
			continue // surfaces again as a DRIFT entry in the main loop below
		}
		content, err := os.ReadFile(file.Path)
		if err != nil {
			return nil, err
		}
		lc := ddlcheck.Resolve(eng.DDL(), string(content))
		// A staging name is skipped — it belongs to nobody, because the
		// file leaves nothing behind under it.
		for _, obj := range lc.LeftBehind {
			if lc.Temporary(obj) {
				continue
			}
			lastWord[obj] = e.Version
		}
		for _, obj := range lc.Destroyed {
			lastWord[obj] = e.Version
		}
	}

	report := &Report{Target: targetName}
	for _, e := range entries {
		file, err := dir.Find(e.Version)
		if err != nil {
			if e.Status == ledger.StatusReverted {
				// The file was renamed/deleted (e.g. split or squashed by a
				// later migration) after this version was marked reverted.
				// There's nothing left to check it against, and the ledger
				// already says its objects shouldn't exist — OK, not DRIFT.
				report.Entries = append(report.Entries, Entry{Version: e.Version, Status: "OK"})
				continue
			}
			report.Entries = append(report.Entries, Entry{Version: e.Version, Status: "DRIFT", Detail: err.Error()})
			continue
		}

		content, err := os.ReadFile(file.Path)
		if err != nil {
			return nil, err
		}
		// What this file leaves behind: its CREATEs plus the names its
		// renames produce. A name the file itself destroys is not on that
		// list's critical path — see the excuse below.
		lc := ddlcheck.Resolve(eng.DDL(), string(content))
		objects := lc.LeftBehind

		status := "OK"
		var details []string

		// Content-hash check: an applied migration whose file was edited
		// after apply is drift even when every object still exists — the
		// DB no longer matches what the file says. Backfilled rows have no
		// hash (recorded before hashing existed) and adopted rows (inferred
		// at adopt time, not observed at apply time) are skipped.
		if e.Status == ledger.StatusApplied && e.ContentSHA256 != "" && e.HashSource != ledger.HashSourceAdopted {
			sum, err := dir.ContentHash(e.Version)
			if err != nil {
				return nil, err
			}
			if sum != e.ContentSHA256 {
				status = "DRIFT"
				details = append(details, "migration file was edited after it was applied (content hash mismatch)")
			}
		}

		for _, obj := range objects {
			// A staging name: this file creates it and then destroys it
			// itself, by dropping it or by renaming it away. Nothing is
			// left under that name, so requiring it to exist would report
			// drift against a schema that matches the file exactly.
			if lc.Temporary(obj) {
				continue
			}
			exists, err := eng.DDL().Exists(db, obj)
			if err != nil {
				return nil, err
			}
			owner, hasOwner := lastWord[obj]
			excused := hasOwner && owner > e.Version
			if e.Status == ledger.StatusApplied && !exists && !excused {
				status = "DRIFT"
				details = append(details, fmt.Sprintf("%s.%s: claimed applied but missing", obj.Schema, obj.Name))
			}
			if e.Status == ledger.StatusReverted && exists {
				status = "DRIFT"
				details = append(details, fmt.Sprintf("%s.%s: claimed reverted but still exists", obj.Schema, obj.Name))
			}
		}

		report.Entries = append(report.Entries, Entry{Version: e.Version, File: file.Filename, Status: status, Detail: strings.Join(details, "; ")})
	}
	return report, nil
}

// collectNoLedger walks every migration file directly and checks whether
// the objects each one leaves behind exist live — the same per-file shape
// internal/repair uses to validate a repair target, applied to every file
// on disk instead of specific requested versions. No content-hash
// comparison is possible without a recorded hash to compare against.
func collectNoLedger(db *sql.DB, eng engine.Engine, migrationsDir, upSuffix, targetName string) (*Report, error) {
	dir, err := migrator.ReadDir(migrationsDir, upSuffix)
	if err != nil {
		return nil, err
	}

	// Same ownership rule as Collect, keyed by file version.
	lastWord := make(map[ddlcheck.ObjectRef]uint64)
	for _, f := range dir.List() {
		raw, err := os.ReadFile(f.Path)
		if err != nil {
			return nil, err
		}
		lc := ddlcheck.Resolve(eng.DDL(), string(raw))
		for _, obj := range lc.LeftBehind {
			if lc.Temporary(obj) {
				continue
			}
			lastWord[obj] = f.Version
		}
		for _, obj := range lc.Destroyed {
			lastWord[obj] = f.Version
		}
	}

	var entries []Entry
	for _, f := range dir.List() {
		raw, err := os.ReadFile(f.Path)
		if err != nil {
			return nil, err
		}
		lc := ddlcheck.Resolve(eng.DDL(), string(raw))
		missing := ""
		for _, obj := range lc.LeftBehind {
			if lc.Temporary(obj) {
				continue
			}
			exists, err := eng.DDL().Exists(db, obj)
			if err != nil {
				return nil, err
			}
			owner, hasOwner := lastWord[obj]
			excused := hasOwner && owner > f.Version
			if !exists && !excused {
				missing = fmt.Sprintf("%s.%s missing", obj.Schema, obj.Name)
				break
			}
		}
		if missing != "" {
			entries = append(entries, Entry{Version: f.Version, File: f.Filename, Status: "DRIFT", Detail: missing})
			continue
		}
		entries = append(entries, Entry{Version: f.Version, File: f.Filename, Status: "OK", Detail: "no ledger — object presence only, no content-hash check possible"})
	}

	return &Report{Target: targetName, Entries: entries}, nil
}
