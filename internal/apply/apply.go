package apply

import (
	"fmt"

	"github.com/dbtools/dbtools/internal/config"
	"github.com/dbtools/dbtools/internal/dbconn"
	"github.com/dbtools/dbtools/internal/ledger"
	"github.com/dbtools/dbtools/internal/migrator"
	"github.com/dbtools/dbtools/internal/statusinfo"
)

// Run resolves targetName's connection string (or uses urlOverride when
// provided — see ResolveURLOrFlag), applies every pending migration,
// records each newly-applied version in the migration ledger, and returns
// the post-apply status. Both `dbtools up` and `dbtools push <target>`
// call this — there is exactly one apply path.
func Run(cfg *config.Config, targetName string, urlOverride string) (*statusinfo.Status, error) {
	url, err := cfg.ResolveURLOrFlag(targetName, urlOverride)
	if err != nil {
		return nil, err
	}

	m, err := migrator.Open(url, cfg.MigrationsDir)
	if err != nil {
		return nil, err
	}
	defer m.Close()

	db, err := dbconn.Open(url)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}
	defer db.Close()

	if err := ledger.Sync(db, m, cfg.MigrationsDir); err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}

	versionBefore, _, hasVersionBefore, err := m.Version()
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}
	all, err := statusinfo.ListMigrationFiles(cfg.MigrationsDir)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}
	newlyPending := statusinfo.ComputePendingVersions(versionBefore, hasVersionBefore, all)

	if _, err := m.Up(); err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}

	for _, v := range newlyPending {
		if err := ledger.SetStatus(db, v, ledger.StatusApplied, "applied via up/push"); err != nil {
			return nil, fmt.Errorf("target %q: %w", targetName, err)
		}
	}

	status, err := statusinfo.Collect(url, cfg.MigrationsDir, targetName)
	if err != nil {
		return nil, fmt.Errorf("target %q: %w", targetName, err)
	}
	return status, nil
}
