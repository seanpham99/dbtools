// Package sqliteengine is the SQLite implementation of the engine seam,
// using the pure-Go modernc.org/sqlite driver (no CGO) and golang-migrate's
// sqlite driver (registered under the "sqlite" scheme via the import in
// internal/migrator/sqlitedriver.go). SQLite is serverless: `dbtools
// start`/`stop` are no-ops and `reset` recreates the database file.
package sqliteengine

import (
	"database/sql"
	"strings"

	_ "modernc.org/sqlite"

	"github.com/dbtools/dbtools/internal/engine"
	"github.com/dbtools/dbtools/internal/generate"
)

func init() {
	engine.Register(SQLite{})
}

// PathFromURL converts a dbtools "sqlite://" connection URL to the
// database file path, mirroring golang-migrate's sqlite driver exactly
// (a literal prefix strip, query string dropped) so both always address
// the same file: sqlite://relative/to.db -> relative/to.db,
// sqlite:///abs/to.db -> /abs/to.db.
func PathFromURL(rawURL string) string {
	p := strings.TrimPrefix(rawURL, "sqlite://")
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	return p
}

// SQLite is the SQLite engine.
type SQLite struct{}

func (SQLite) Name() string { return "sqlite" }

// Open opens the database file named by rawURL, creating it if absent
// (SQLite's native behavior).
func (SQLite) Open(rawURL string) (*sql.DB, error) {
	return sql.Open("sqlite", PathFromURL(rawURL))
}

func (SQLite) DDL() engine.DDLDialect { return ddl{} }

func (SQLite) Ledger() engine.LedgerStore { return ledgerStore{} }

func (SQLite) Introspect(db *sql.DB, excludeList []string) ([]generate.TableSchema, []string, error) {
	return introspect(db, excludeList)
}
