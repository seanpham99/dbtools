package migrator

import (
	"database/sql"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/database/sqlserver"
	_ "github.com/microsoft/go-mssqldb"
)

// goBatchSeparator matches a line containing only sqlcmd/SSMS's GO batch
// separator. GO is a client-tool convention, not real T-SQL — no database
// driver understands it, so it must be split out before executing.
var goBatchSeparator = regexp.MustCompile(`(?im)^\s*GO\s*$`)

func init() {
	database.Register("mssql", &goSplitDriver{})
}

// goSplitDriver wraps golang-migrate's sqlserver driver (reusing its
// locking/version-table/connection logic unchanged) and overrides only
// Run: it splits each migration file on GO batch separators and executes
// the batches individually. golang-migrate's own Run sends an entire
// migration file as a single exec, which breaks on any file containing
// multiple CREATE PROCEDURE/VIEW statements that reuse local variable
// names across what used to be separate sqlcmd batches — and simply
// deleting GO merges those batches together, causing the same collision.
// Registered under the "mssql" scheme (not "sqlserver", which
// golang-migrate's own package still self-registers on import) so
// dbtools's connection URLs use mssql:// to route through this wrapper.
type goSplitDriver struct {
	inner  database.Driver
	rawURL string
}

func (d *goSplitDriver) Open(rawURL string) (database.Driver, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parsing URL: %w", err)
	}
	if u.Scheme != "mssql" && u.Scheme != "sqlserver" {
		return nil, fmt.Errorf("expected mssql:// or sqlserver:// URL scheme, got %q", u.Scheme)
	}
	u.Scheme = "sqlserver"
	q := u.Query()
	q.Del("x-migrations-table")
	u.RawQuery = q.Encode()

	inner, err := (&sqlserver.SQLServer{}).Open(u.String())
	if err != nil {
		return nil, err
	}
	return &goSplitDriver{inner: inner, rawURL: u.String()}, nil
}

func (d *goSplitDriver) Close() error  { return d.inner.Close() }
func (d *goSplitDriver) Lock() error   { return d.inner.Lock() }
func (d *goSplitDriver) Unlock() error { return d.inner.Unlock() }

// SetVersion delegates to the stock driver: it writes the version table
// through the same [SCHEMA_NAME()].[MigrationsTable] path it reads from,
// honouring x-migrations-table.
func (d *goSplitDriver) SetVersion(version int, dirty bool) error {
	return d.inner.SetVersion(version, dirty)
}

func (d *goSplitDriver) Version() (int, bool, error) { return d.inner.Version() }
func (d *goSplitDriver) Drop() error                 { return d.inner.Drop() }

// Run executes every GO-delimited batch in migration inside a single
// transaction, so a failure partway through a multi-batch migration file
// leaves zero partial changes.
func (d *goSplitDriver) Run(migration io.Reader) error {
	data, err := io.ReadAll(migration)
	if err != nil {
		return err
	}

	db, err := sql.Open("sqlserver", d.rawURL)
	if err != nil {
		return err
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return err
	}

	for _, batch := range goBatchSeparator.Split(string(data), -1) {
		trimmed := strings.TrimSpace(batch)
		if trimmed == "" {
			continue
		}
		if _, err := tx.Exec(batch); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
