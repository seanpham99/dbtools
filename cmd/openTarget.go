package cmd

import (
	"database/sql"
	"fmt"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/migrator"
)

// OpenTarget resolves a target's connection string (or --url override),
// validates its engine, opens the database connection, and builds the
// migration runner. It is the single shared preamble for every command that
// acts on one named target — verify, repair, push, generate, apply.
//
// The caller owns closing db. The returned url is what the caller should
// pass to apply.Run/statusinfo.Collect etc. so the same resolution logic is
// never duplicated with drift (the guards in this file are only as
// trustworthy as the one code path that feeds them).
func OpenTarget(cfg *config.Config, targetName, urlOverride string) (eng engine.Engine, db *sql.DB, r *migrator.Runner, url string, err error) {
	url, err = cfg.ResolveURLOrFlag(targetName, urlOverride)
	if err != nil {
		return nil, nil, nil, "", err
	}
	eng, err = engine.ForTarget(cfg.EngineName(targetName), url)
	if err != nil {
		return nil, nil, nil, "", err
	}
	if t, ok := cfg.Targets[targetName]; !ok || !t.Protected {
		if err := engine.EnsureDatabase(eng, url); err != nil {
			return nil, nil, nil, "", fmt.Errorf("target %q: %w", targetName, err)
		}
	}
	db, err = eng.Open(url)
	if err != nil {
		return nil, nil, nil, "", err
	}
	migrationsDir, upSuffix, ledgerTable := config.ResolveDefaults(cfg.MigrationsDir, cfg.Migrations.UpSuffix, cfg.LedgerTableName())
	dir, err := migrator.ReadDir(migrationsDir, upSuffix)
	if err != nil {
		db.Close()
		return nil, nil, nil, "", err
	}
	return eng, db, migrator.NewRunner(eng, db, dir, ledgerTable), url, nil
}

// requireUnprotected refuses commands that mutate a protected target
// (status/verify are read-only and allowed). protected is declared in
// dbtools.toml as targets.<name>.protected = true — the config author's
// explicit "this environment is not for routine writes" marker.
func requireUnprotected(cfg *config.Config, targetName string) error {
	t, ok := cfg.Targets[targetName]
	if !ok {
		return fmt.Errorf("unknown target %q", targetName)
	}
	if t.Protected {
		return fmt.Errorf("target %q is protected (declared protected in dbtools.toml) — refusing to modify it; remove the protected flag to write", targetName)
	}
	return nil
}
