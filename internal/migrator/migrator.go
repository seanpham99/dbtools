package migrator

import (
	"errors"
	"fmt"
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

// ApplyFile applies the single migration f via m.Run: it feeds f's own
// contents to the database driver and moves the cursor to f.Version,
// bypassing golang-migrate's source-index walk (Steps/Up). The walk is
// unsafe as an apply path because it guards on versionExists(cursor) —
// golang-migrate refuses to advance when the *current* version's file no
// longer exists on disk, and its os.ErrNotExist for that case is
// indistinguishable from "no next migration". Once an applied file is
// renamed or removed from migrationsDir, Steps(1) therefore returns
// "no change" while PendingAfter still lists real pending files —
// exactly the silent skip this replaces. Callers own the pending list:
// apply.Run iterates dir.PendingAfter and applies each file explicitly.
func (mg *Migrator) ApplyFile(f File) error {
	body, err := os.Open(f.Path)
	if err != nil {
		return fmt.Errorf("opening migration file %s: %w", f.Filename, err)
	}
	migr, err := migrate.NewMigration(body, f.Filename, uint(f.Version), int(f.Version))
	if err != nil {
		body.Close()
		return fmt.Errorf("preparing migration %s: %w", f.Filename, err)
	}
	if err := mg.m.Run(migr); err != nil {
		return fmt.Errorf("applying migration %s: %w", f.Filename, err)
	}
	return nil
}

// StepDown rolls back the single most recently applied migration using its .down.sql file.
// applied is false if there was nothing to do. Caveat: golang-migrate's
// readDown emits the same os.ErrNotExist for a missing *cursor* file
// (versionExists) as for "no previous migration" — this wrapper can't
// distinguish them, so a deleted applied file silently stops the revert.
// Tolerable for `down` (best-effort cleanup); never gate correctness on it.
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
