package testdb

import (
	"database/sql"
	"fmt"
	"net/url"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/microsoft/go-mssqldb"
	_ "modernc.org/sqlite"

	"github.com/seanpham99/dbtools/internal/redact"
)

// Open opens a database connection for tests across MSSQL, Postgres, SQLite, or MySQL.
func Open(rawURL string) (*sql.DB, error) {
	if strings.HasPrefix(rawURL, "mysql://") {
		dsn := strings.TrimPrefix(rawURL, "mysql://")
		return sql.Open("mysql", dsn)
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parsing URL %q: %w", redact.URL(rawURL), redact.ParseError(err))
	}
	switch u.Scheme {
	case "mssql":
		u.Scheme = "sqlserver"
		q := u.Query()
		q.Del("x-migrations-table")
		u.RawQuery = q.Encode()
		return sql.Open("sqlserver", u.String())
	case "sqlserver":
		return sql.Open("sqlserver", rawURL)
	case "postgres", "postgresql":
		return sql.Open("postgres", rawURL)
	case "sqlite", "file":
		path := strings.TrimPrefix(rawURL, "sqlite://")
		return sql.Open("sqlite", path)
	case "mysql":
		return sql.Open("mysql", strings.TrimPrefix(rawURL, "mysql://"))
	default:
		return nil, fmt.Errorf("unsupported testdb scheme: %s", u.Scheme)
	}
}

// ResetTracking drops golang-migrate's version-tracking table and the
// migration ledger, so an integration test starts from a clean slate
// regardless of what any other test left behind in the same shared
// database — both tables live in the target database, not scoped per
// migrations-directory.
func ResetTracking(rawURL string) error {
	db, err := Open(rawURL)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec("DROP TABLE IF EXISTS schema_migrations"); err != nil {
		return err
	}
	_, err = db.Exec("DROP TABLE IF EXISTS dbtools_migration_history")
	return err
}
