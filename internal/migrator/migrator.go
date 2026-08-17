package migrator

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// Migrator wraps a golang-migrate instance for one target's database URL
// and one local migrations directory.
type Migrator struct {
	m *migrate.Migrate
}

// Open connects to databaseURL and points at the plain-SQL migration files
// in migrationsDir.
func Open(databaseURL, migrationsDir string) (*Migrator, error) {
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
