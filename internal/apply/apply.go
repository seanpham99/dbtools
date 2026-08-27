package apply

import (
	"context"
	"fmt"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine"
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
	ctx := context.Background()
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

	migrationsDir, upSuffix, ledgerTable := config.ResolveDefaults(cfg.MigrationsDir, cfg.Migrations.UpSuffix, cfg.LedgerTableName())

	db, err := eng.Open(url)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}
	defer db.Close()

	dir, err := migrator.ReadDir(migrationsDir, upSuffix)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}

	// The runner holds the migration lock for the whole apply and records
	// each version as applying before its SQL runs, so a run that dies
	// partway is visible to the next one rather than silently resumed.
	if _, err := migrator.NewRunner(eng, db, dir, ledgerTable).Up(ctx); err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}

	status, err := statusinfo.Collect(url, cfg.EngineName(targetName), migrationsDir, upSuffix, ledgerTable, targetName)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}
	return status, nil
}
