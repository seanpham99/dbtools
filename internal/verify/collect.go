package verify

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

// Collect checks every ledger row in db against migrationsDir's files:
// versions marked "applied" must have every object their migration creates
// actually present AND the migration file's content must still match the
// hash recorded when it was applied; versions marked "reverted" must not.
func Collect(db *sql.DB, eng engine.Engine, migrationsDir, targetName string) (*Report, error) {
	entries, err := eng.Ledger().List(db)
	if err != nil {
		return nil, err
	}

	// Objects any applied migration explicitly DROPs are expected-absent.
	// Track drops per-object but allow a later migration to re-create the
	// object: a version's CREATE is only excused by a drop that came
	// BEFORE it and was not itself superseded by a re-create. (A global
	// unordered union would mark a re-created object as permanently
	// dropped and hide genuine later disappearance.)
	droppedBefore := make(map[ddlcheck.ObjectRef]uint64) // object -> version that dropped it
	for _, e := range entries {
		if e.Status != ledger.StatusApplied {
			continue
		}
		filename, err := migrator.FindMigrationFile(migrationsDir, e.Version)
		if err != nil {
			continue // surfaces again as a DRIFT entry in the main loop below
		}
		content, err := os.ReadFile(filepath.Join(migrationsDir, filename))
		if err != nil {
			return nil, err
		}
		for _, obj := range eng.DDL().ExtractDroppedObjects(string(content)) {
			droppedBefore[obj] = e.Version
		}
		// If this migration itself re-creates the object, it cancels any
		// earlier drop: a later genuine disappearance must be DRIFT.
		for _, obj := range eng.DDL().ExtractObjects(string(content)) {
			delete(droppedBefore, obj)
		}
	}

	report := &Report{Target: targetName}
	for _, e := range entries {
		filename, err := migrator.FindMigrationFile(migrationsDir, e.Version)
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

		content, err := os.ReadFile(filepath.Join(migrationsDir, filename))
		if err != nil {
			return nil, err
		}
		objects := eng.DDL().ExtractObjects(string(content))

		status := "OK"
		var details []string

		// Content-hash check: an applied migration whose file was edited
		// after apply is drift even when every object still exists — the
		// DB no longer matches what the file says. Backfilled rows have no
		// hash (recorded before hashing existed) and are skipped.
		if e.Status == ledger.StatusApplied && e.ContentSHA256 != "" {
			sum, err := migrator.ContentHash(migrationsDir, e.Version)
			if err != nil {
				return nil, err
			}
			if sum != e.ContentSHA256 {
				status = "DRIFT"
				details = append(details, "migration file was edited after it was applied (content hash mismatch)")
			}
		}

		for _, obj := range objects {
			exists, err := eng.DDL().Exists(db, obj)
			if err != nil {
				return nil, err
			}
			droppedAt, wasDropped := droppedBefore[obj]
			excused := wasDropped && droppedAt > e.Version
			if e.Status == ledger.StatusApplied && !exists && !excused {
				status = "DRIFT"
				details = append(details, fmt.Sprintf("%s.%s: claimed applied but missing", obj.Schema, obj.Name))
			}
			if e.Status == ledger.StatusReverted && exists {
				status = "DRIFT"
				details = append(details, fmt.Sprintf("%s.%s: claimed reverted but still exists", obj.Schema, obj.Name))
			}
		}

		report.Entries = append(report.Entries, Entry{Version: e.Version, File: filename, Status: status, Detail: strings.Join(details, "; ")})
	}
	return report, nil
}
