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

	if t, ok := cfg.Targets[targetName]; !ok || !t.Protected {
		if err := engine.EnsureDatabase(eng, url); err != nil {
			return nil, fmt.Errorf("target %q: %w", targetName, err)
		}
	}

	migrationsDir, upSuffix, ledgerTable := config.ResolveDefaults(cfg.MigrationsDir, cfg.Migrations.UpSuffix, cfg.Ledger.Table)

	// migrator.Open wraps golang-migrate, whose own file source driver
	// looks for "<version>_<name>.up.sql" unconditionally — it has no way
	// to honor a custom up_suffix. A non-default suffix would make our
	// own dir.PendingAfter (below) report files as pending that
	// golang-migrate's m.Step() can never find, silently applying
	// nothing while looking like success. Fail closed instead: today,
	// up_suffix is only honored by the read-only commands and `adopt`.
	if upSuffix != config.DefaultUpSuffix {
		return nil, fmt.Errorf("target %q: migrations.up_suffix=%q is not supported for applying migrations (up/push) — golang-migrate's execution engine requires %q; the read-only commands and `dbtools adopt` honor the custom suffix, but up/push cannot yet", targetName, upSuffix, config.DefaultUpSuffix)
	}

	m, err := migrator.Open(url, migrationsDir)
	if err != nil {
		return nil, err
	}
	defer m.Close()

	db, err := eng.Open(url)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}
	defer db.Close()

	if err := eng.Ledger().EnsureSchema(db, ledgerTable); err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}

	dir, err := migrator.ReadDir(migrationsDir, upSuffix)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}

	versionBefore, dirty, hasVersionBefore, err := m.Version()
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}
	if dirty {
		return nil, fmt.Errorf("target %q: migration cursor is dirty at version %d (a previous apply failed partway); run `dbtools repair %s` to resolve it", targetName, versionBefore, targetName)
	}

	pending := dir.PendingAfter(versionBefore, hasVersionBefore)
	for _, expected := range pending {
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
		if versionAfter != expected.Version {
			return nil, fmt.Errorf("target %q: migration cursor advanced unexpectedly (expected version %d, got %d)", targetName, expected.Version, versionAfter)
		}

		hash, err := dir.ContentHash(versionAfter)
		if err != nil {
			return nil, fmt.Errorf("target %q: %w", targetName, err)
		}
		if err := eng.Ledger().SetStatusWithHash(db, versionAfter, ledger.StatusApplied, "applied via up/push", hash, ledgerTable); err != nil {
			return nil, fmt.Errorf("target %q: %w", targetName, err)
		}
	}

	status, err := statusinfo.Collect(url, migrationsDir, upSuffix, targetName)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}
	return status, nil
}
