// Package mysqlengine is the MySQL implementation of the engine seam. Its
// golang-migrate driver self-registers under the "mysql" scheme in
// internal/migrator/mysqldriver.go.
package mysqlengine

import (
	"database/sql"

	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/generate"
)

func init() {
	engine.Register(MySQL{})
}

// MySQL is the MySQL engine.
type MySQL struct{}

func (MySQL) Name() string { return "mysql" }

func (MySQL) Open(rawURL string) (*sql.DB, error) { return Open(rawURL) }

func (MySQL) DDL() engine.DDLDialect { return mysqlDDL{} }

func (MySQL) Ledger() engine.LedgerStore { return mysqlLedgerStore{} }

func (MySQL) Introspect(db *sql.DB, excludeList []string) ([]generate.TableSchema, []string, error) {
	return introspect(db, excludeList)
}
