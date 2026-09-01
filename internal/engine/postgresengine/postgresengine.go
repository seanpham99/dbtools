// Package postgresengine is the Postgres implementation of the engine
// seam. Unlike MSSQL, Postgres needs no batch splitting — golang-migrate's
// own postgres driver (registered under the "postgres" scheme via the
// import in internal/migrator/pgdriver.go) executes migration files as-is.
package postgresengine

import (
	"database/sql"

	"github.com/lib/pq"

	"github.com/seanpham99/dbtools/internal/dburl"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/generate"
	"github.com/seanpham99/dbtools/internal/logger"
)

func init() {
	engine.Register(Postgres{})
}

// Postgres is the Postgres engine.
type Postgres struct{}

func (Postgres) Name() string { return "postgres" }

// Open opens a database/sql connection via lib/pq, which accepts
// postgres:// URLs natively — no scheme rewriting needed.
//
// Migration-tool x- parameters are stripped first: lib/pq validates unknown
// query parameters as server settings and fails the connection with
// `unrecognized configuration parameter "x-migrations-table"`.
//
// A notice handler is installed so RAISE NOTICE from a migration reaches
// the log. lib/pq discards notices unless something is listening, and
// migrations use them to report what they found — which is the only
// feedback available when dbtools runs as a private-network job whose
// output is its log (#60).
//
// Everything is logged, including routine schema-maintenance notices:
// suppression here is global to the connection, so filtering (say)
// "already exists, skipping" would also swallow a migration that raises
// the same text on purpose. EnsureSchema instead avoids emitting routine
// notices at all (see ledger.go).
func (Postgres) Open(rawURL string) (*sql.DB, error) {
	clean := dburl.StripCustomParams(rawURL)
	connector, err := pq.NewConnector(clean)
	if err != nil {
		// Fall back rather than fail: NewConnector is stricter than
		// sql.Open about some DSN forms, and losing notices is better
		// than refusing to connect at all.
		return sql.Open("postgres", clean)
	}
	return sql.OpenDB(pq.ConnectorWithNoticeHandler(connector, func(n *pq.Error) {
		logger.Infof("postgres: %s: %s", n.Severity, n.Message)
	})), nil
}

func (Postgres) DDL() engine.DDLDialect { return ddl{} }

func (Postgres) Ledger() engine.LedgerStore { return ledgerStore{} }

func (Postgres) Introspect(db *sql.DB, excludeList []string) ([]generate.TableSchema, []string, error) {
	return introspect(db, excludeList)
}
