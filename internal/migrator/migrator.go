package migrator

import (
	"errors"
	"fmt"
	"net/url"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// Migrator wraps a golang-migrate instance for one target's database URL
// and one local migrations directory.
type Migrator struct {
	m *migrate.Migrate
}

// Open connects to databaseURL and points at the plain-SQL migration files
// in migrationsDir.
//
// Postgres URLs are routed through a driver wrapper that resets
// session-level state per migration (see pgResetDriver) — a pg_dump
// baseline's set_config('search_path',”,false) would otherwise poison
// every later migration in the same run. MSSQL URLs route through the
// GO-splitting wrapper via their mssql:// scheme. Everything else uses
// golang-migrate's scheme lookup.
func Open(databaseURL, migrationsDir string) (*Migrator, error) {
	scheme := ""
	if u, err := url.Parse(databaseURL); err == nil {
		scheme = u.Scheme
	}
	if scheme == "postgres" || scheme == "postgresql" {
		drv, err := openPostgresResetDriver(databaseURL)
		if err != nil {
			return nil, err
		}
		src, err := iofs.New(os.DirFS(migrationsDir), ".")
		if err != nil {
			return nil, fmt.Errorf("opening migrations source: %w", err)
		}
		m, err := migrate.NewWithInstance("iofs", src, "pgreset", drv)
		if err != nil {
			return nil, fmt.Errorf("opening migrator: %w", err)
		}
		return &Migrator{m: m}, nil
	}

	m, err := migrate.New("file://"+migrationsDir, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("opening migrator: %w", err)
	}
	return &Migrator{m: m}, nil
}

// Up applies all pending migrations. applied is false if there was nothing
// to do (golang-migrate's ErrNoChange, not treated as an error here).
func (mg *Migrator) Up() (applied bool, err error) {
	err = mg.m.Up()
	if errors.Is(err, migrate.ErrNoChange) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("applying migrations: %w", err)
	}
	return true, nil
}

// Step applies the single next pending migration. applied is false if there
// was nothing to do. Callers that need to record per-migration side effects
// (e.g. the ledger) must use Step in a loop instead of Up, so a migration
// that fails partway through a batch leaves the already-applied ones
// recorded.
//
// golang-migrate's Steps(1) reports os.ErrNotExist ("file does not exist")
// when the last migration has already been applied and there is no "next"
// file — treat that as no-change, same as ErrNoChange.
func (mg *Migrator) Step() (applied bool, err error) {
	err = mg.m.Steps(1)
	if errors.Is(err, migrate.ErrNoChange) || errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("applying next migration: %w", err)
	}
	return true, nil
}

// Version reports the current migration version. hasVersion is false if no
// migration has ever been applied (golang-migrate's ErrNilVersion).
func (mg *Migrator) Version() (version uint64, dirty bool, hasVersion bool, err error) {
	v, d, err := mg.m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return 0, false, false, nil
	}
	if err != nil {
		return 0, false, false, fmt.Errorf("reading version: %w", err)
	}
	return uint64(v), d, true, nil
}

// Stamp marks version as the current applied migration WITHOUT executing
// its SQL. Useful when a target already has the schema a migration file
// describes and you only need to record it as applied.
func (mg *Migrator) Stamp(version uint64) error {
	if err := mg.m.Force(int(version)); err != nil {
		return fmt.Errorf("stamping version %d: %w", version, err)
	}
	return nil
}

// Close releases the underlying database connection.
func (mg *Migrator) Close() error {
	sourceErr, dbErr := mg.m.Close()
	if dbErr != nil {
		return dbErr
	}
	return sourceErr
}
