package mysqlengine

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/go-sql-driver/mysql"
)

// dsnFromURL converts a dbtools "mysql://" target URL into the DSN
// github.com/go-sql-driver/mysql expects: golang-migrate's own mysql
// driver does the same TrimPrefix + ParseDSN (see
// database/mysql/mysql.go's urlToMySQLConfig), so dbtools's raw mysql://
// URLs already match the format golang-migrate itself requires.
//
// ParseTime is always forced true: without it, DATETIME/TIMESTAMP columns
// scan as []byte instead of time.Time/sql.NullTime, which would silently
// break the ledger's recorded_at column (internal/ledger.Entry.RecordedAt
// is *time.Time) for every caller who forgot the query param.
func dsnFromURL(rawURL string) (string, error) {
	raw := strings.TrimPrefix(rawURL, "mysql://")
	cfg, err := mysql.ParseDSN(raw)
	if err != nil {
		return "", fmt.Errorf("parsing mysql DSN: %w", err)
	}
	cfg.ParseTime = true
	return cfg.FormatDSN(), nil
}

// Open opens a direct database/sql connection to rawURL (a dbtools
// "mysql://"-scheme connection string), for callers that need raw SQL
// access alongside golang-migrate's own tracked connection.
func Open(rawURL string) (*sql.DB, error) {
	dsn, err := dsnFromURL(rawURL)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database connection: %w", err)
	}
	return db, nil
}
