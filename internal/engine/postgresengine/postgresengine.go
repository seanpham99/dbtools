// Package postgresengine is the Postgres implementation of the engine
// seam. Unlike MSSQL, Postgres needs no batch splitting — golang-migrate's
// own postgres driver (registered under the "postgres" scheme via the
// import in internal/migrator/pgdriver.go) executes migration files as-is.
package postgresengine

import (
	"database/sql"

	_ "github.com/lib/pq"

	"github.com/seanpham99/dbtools/internal/dburl"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/generate"
)

func init() {
	engine.Register(Postgres{})
}

// Postgres is the Postgres engine.
type Postgres struct{}

func (Postgres) Name() string { return "postgres" }

// Open opens a raw database/sql connection via lib/pq, which accepts
// postgres:// URLs natively — no scheme rewriting needed.
//
// golang-migrate's x- parameters are stripped first: lib/pq validates
// unknown query parameters as server settings and fails the connection
// with `unrecognized configuration parameter "x-migrations-table"`.
func (Postgres) Open(rawURL string) (*sql.DB, error) {
	return sql.Open("postgres", dburl.StripCustomParams(rawURL))
}

func (Postgres) DDL() engine.DDLDialect { return ddl{} }

func (Postgres) Ledger() engine.LedgerStore { return ledgerStore{} }

func (Postgres) Introspect(db *sql.DB, excludeList []string) ([]generate.TableSchema, []string, error) {
	return introspect(db, excludeList)
}
