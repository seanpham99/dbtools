package apply

import (
	"fmt"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/ledger"
	"github.com/seanpham99/dbtools/internal/migrator"
	"github.com/seanpham99/dbtools/internal/statusinfo"
)

// Run resolves targetName's connection string (or uses urlOverride when
// provided — see ResolveURLOrFlag), applies every pending migration one
// step at a time, records each newly-applied version in the migration
// ledger (with its file's content hash), and returns the post-apply
// status. Both `dbtools up` and `dbtools push <target>` call this — there
// is exactly one apply path.
func Run(cfg *config.Config, targetName string, urlOverride string) (*statusinfo.Status, error) {
	url, err := cfg.ResolveURLOrFlag(targetName, urlOverride)
	if err != nil {
		return nil, err
	}

	eng, err := engine.ForTarget(cfg.EngineName(targetName), url)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}

	m, err := migrator.Open(url, cfg.MigrationsDir)
	if err != nil {
		return nil, err
	}
	defer m.Close()

	db, err := eng.Open(url)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}
	defer db.Close()

	if err := eng.Ledger().Sync(db, m, cfg.MigrationsDir); err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}

	// Apply one migration at a time, recording the ledger row immediately
	// after each success. If migration N fails, migrations 1..N-1 are
	// applied in the database AND recorded — the next run sees them as
	// applied and re-attempts only N. (An all-or-nothing m.Up() would
	// leave the database advanced and the ledger empty, which is exactly
	// the lie this ledger exists to prevent.)
	for {
		versionBefore, dirty, hasVersionBefore, err := m.Version()
		if err != nil {
			return nil, fmt.Errorf("target %q: %w", targetName, err)
		}
		if dirty {
			return nil, fmt.Errorf("target %q: migration cursor is dirty at version %d (a previous apply failed partway); run `dbtools repair %s` to resolve it", targetName, versionBefore, targetName)
		}

		applied, err := m.Step()
		if err != nil {
			return nil, fmt.Errorf("target %q: %w", targetName, err)
		}
		if !applied {
			break // no pending migrations left
		}

		versionAfter, _, _, err := m.Version()
		if err != nil {
			return nil, fmt.Errorf("target %q: %w", targetName, err)
		}
		// Step applies exactly the next pending version; guard against a
		// driver whose cursor jumps in unexpected ways.
		if versionAfter != versionBefore+1 && !(!hasVersionBefore && versionAfter > versionBefore) {
			return nil, fmt.Errorf("target %q: migration cursor advanced unexpectedly (was %d, now %d)", targetName, versionBefore, versionAfter)
		}
		hash, err := migrator.ContentHash(cfg.MigrationsDir, versionAfter)
		if err != nil {
			return nil, fmt.Errorf("target %q: %w", targetName, err)
		}
		if err := eng.Ledger().SetStatusWithHash(db, versionAfter, ledger.StatusApplied, "applied via up/push", hash); err != nil {
			return nil, fmt.Errorf("target %q: %w", targetName, err)
		}
	}

	status, err := statusinfo.Collect(url, cfg.MigrationsDir, targetName)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}
	return status, nil
}
