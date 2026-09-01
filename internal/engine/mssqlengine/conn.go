package mssqlengine

import (
	"database/sql"
	"fmt"
	"net/url"

	_ "github.com/microsoft/go-mssqldb"

	"github.com/seanpham99/dbtools/internal/redact"
)

// RewriteToSQLServerScheme rewrites rawURL's scheme from dbtools's own
// "mssql://" connection-string convention to "sqlserver://", the only
// scheme go-mssqldb's URL parser recognizes (see
// internal/migrator/godriver.go for why dbtools uses "mssql://" for its
// own driver registration).
func RewriteToSQLServerScheme(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parsing mssql:// URL %q: %w", redact.URL(rawURL), redact.ParseError(err))
	}
	u.Scheme = "sqlserver"
	return u.String(), nil
}

// Open opens a direct database/sql connection to rawURL (a dbtools
// "mssql://"-scheme connection string), for callers that need raw SQL
// access alongside golang-migrate's own tracked connection.
func Open(rawURL string) (*sql.DB, error) {
	sqlserverURL, err := RewriteToSQLServerScheme(rawURL)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlserver", sqlserverURL)
	if err != nil {
		return nil, fmt.Errorf("opening database connection: %w", err)
	}
	return db, nil
}
