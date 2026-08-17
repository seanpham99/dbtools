package verify

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dbtools/dbtools/internal/ddlcheck"
	"github.com/dbtools/dbtools/internal/engine"
	"github.com/dbtools/dbtools/internal/ledger"
	"github.com/dbtools/dbtools/internal/migrator"
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
// actually present; versions marked "reverted" must not.
func Collect(db *sql.DB, eng engine.Engine, migrationsDir, targetName string) (*Report, error) {
	entries, err := eng.Ledger().List(db)
	if err != nil {
		return nil, err
	}

	// Objects any applied migration explicitly DROPs are expected-absent — an
	// earlier migration's CREATE for the same object no longer existing is
	// intentional removal, not drift (e.g. a legacy table created by an early
	// migration and legitimately dropped by a later one).
	dropped := make(map[ddlcheck.ObjectRef]bool)
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
			dropped[obj] = true
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
		for _, obj := range objects {
			exists, err := eng.DDL().Exists(db, obj)
			if err != nil {
				return nil, err
			}
			if e.Status == ledger.StatusApplied && !exists && !dropped[obj] {
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
