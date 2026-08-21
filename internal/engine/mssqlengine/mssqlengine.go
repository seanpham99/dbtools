// Package mssqlengine is the MSSQL implementation of the engine seam.
// Its golang-migrate driver — the GO-batch-splitting wrapper — registers
// under the "mssql" scheme in internal/migrator/godriver.go.
package mssqlengine

import (
	"database/sql"

	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/generate"
)

func init() {
	engine.Register(MSSQL{})
}

// MSSQL is the MSSQL engine.
type MSSQL struct{}

func (MSSQL) Name() string { return "mssql" }

func (MSSQL) Open(rawURL string) (*sql.DB, error) { return Open(rawURL) }

func (MSSQL) DDL() engine.DDLDialect { return mssqlDDL{} }

func (MSSQL) Ledger() engine.LedgerStore { return mssqlLedgerStore{} }

func (MSSQL) Introspect(db *sql.DB, excludeList []string) ([]generate.TableSchema, []string, error) {
	return Introspect(db, excludeList)
}
