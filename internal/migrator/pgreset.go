package migrator

import (
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/database/postgres"
)

// openPostgresResetDriver builds the postgres driver for rawURL and wraps
// it with the per-migration session reset (see pgResetDriver). Called by
// migrator.Open whenever the URL scheme is postgres:// or postgresql://.
func openPostgresResetDriver(rawURL string) (database.Driver, error) {
	purl, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parsing %q: %w", rawURL, err)
	}
	// WithInstance needs the *sql.DB the driver will use; build it the
	// same way the postgres package does (strip custom query params).
	cleanURL := migrate.FilterCustomQuery(purl).String()
	db, err := sql.Open("postgres", cleanURL)
	if err != nil {
		return nil, err
	}
	inner, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("opening postgres driver: %w", err)
	}
	return &pgResetDriver{inner: inner}, nil
}

// pgResetDriver wraps golang-migrate's postgres driver and resets
// session-level state before every migration. golang-migrate reuses one
// connection for the whole run, so any migration that sets session state
// — set_config('search_path', ...), SET client_min_messages, SET ROLE —
// silently poisons every later migration in the same run. Every
// pg_dump-generated baseline emits exactly that (its header is
// `set_config('search_path',”,false)`), and a later migration that
// assumes the default search_path then resolves the wrong schema.
//
// Resetting search_path and client_min_messages per migration is the
// cheapest correct fix: it makes each file behave as if it ran in a
// fresh session without paying for a new connection per file.
type pgResetDriver struct {
	inner database.Driver
}

func (d *pgResetDriver) Close() error  { return d.inner.Close() }
func (d *pgResetDriver) Lock() error   { return d.inner.Lock() }
func (d *pgResetDriver) Unlock() error { return d.inner.Unlock() }

// Open is never called: migrator.Open builds this driver via
// postgres.WithInstance and NewWithInstance, not by scheme lookup. It
// exists only to satisfy the database.Driver interface.
func (d *pgResetDriver) Open(rawURL string) (database.Driver, error) {
	return openPostgresResetDriver(rawURL)
}

func (d *pgResetDriver) SetVersion(version int, dirty bool) error {
	return d.inner.SetVersion(version, dirty)
}

func (d *pgResetDriver) Version() (int, bool, error) { return d.inner.Version() }
func (d *pgResetDriver) Drop() error                 { return d.inner.Drop() }

// Run resets the session to a clean default, then executes the migration
// file. The version-table bookkeeping (SetVersion) runs on the same
// connection but outside Run, so it still sees the default schema.
func (d *pgResetDriver) Run(migration io.Reader) error {
	if err := d.inner.Run(io.MultiReader(
		strings.NewReader("SET search_path TO public; RESET client_min_messages;"),
		migration,
	)); err != nil {
		return fmt.Errorf("running migration: %w", err)
	}
	return nil
}
