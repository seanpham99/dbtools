package migrator

import (
	"errors"
	"fmt"
	"os"
	"strings"

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
	scheme := SchemeOf(databaseURL)
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
	if scheme == "mysql" {
		databaseURL = ensureMySQLMultiStatements(databaseURL)
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

// StepDown rolls back the single most recently applied migration using its .down.sql file.
// applied is false if there was nothing to do.
func (mg *Migrator) StepDown() (applied bool, err error) {
	err = mg.m.Steps(-1)
	if errors.Is(err, migrate.ErrNoChange) || errors.Is(err, os.ErrNotExist) || errors.Is(err, migrate.ErrNilVersion) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reverting migration: %w", err)
	}
	return true, nil
}

// Down rolls back all applied migrations.
func (mg *Migrator) Down() (applied bool, err error) {
	err = mg.m.Down()
	if errors.Is(err, migrate.ErrNoChange) || errors.Is(err, migrate.ErrNilVersion) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reverting migrations: %w", err)
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
		return 0, false, false, fmt.Errorf("reading version: %w", explainCursorCollision(err))
	}
	return uint64(v), d, true, nil
}

// explainCursorCollision turns the raw driver error produced by a version
// cursor that collides with somebody else's table into an actionable one.
//
// golang-migrate keeps its cursor in a (version, dirty) table it calls
// schema_migrations by default. That is also what golang-migrate itself,
// Rails, Supabase and many hand-rolled runners name their *ledger*, and
// those tables are shaped differently — commonly a text version column and
// no dirty column at all. Pointed at one of those, every command that reads
// the version fails with `column "dirty" does not exist`, which says
// nothing about the actual problem or the fix.
func explainCursorCollision(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	if !strings.Contains(msg, `"dirty"`) || !strings.Contains(msg, "does not exist") {
		return err
	}
	return fmt.Errorf("%w\n\n"+
		"This looks like a version-cursor table collision: the table dbtools uses for its\n"+
		"version cursor already exists and belongs to another migration tool (it has no\n"+
		"\"dirty\" column, so it is not golang-migrate's).\n\n"+
		"Point the cursor at a table of its own in dbtools.toml:\n\n"+
		"    [ledger]\n"+
		"    cursor_table = \"dbtools_schema_version\"\n\n"+
		"then re-run. To import the other tool's history afterwards: dbtools adopt <target>.",
		err)
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

// Force sets version as the current applied migration version and clears any
// dirty flag in the version-tracking table without executing migration SQL.
func (mg *Migrator) Force(version uint64) error {
	if err := mg.m.Force(int(version)); err != nil {
		return fmt.Errorf("forcing version %d: %w", version, err)
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
