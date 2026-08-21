// Package mssqlengine is the MSSQL implementation of the engine seam —
// the reference implementation future engines (Postgres, SQLite) mirror.
// It delegates to the packages that have always held dbtools's MSSQL
// behavior: dbconn (connections), ddlcheck (DDL parsing + existence),
// ledger (ledger SQL), and generate (introspection). Its golang-migrate
// driver — the GO-batch-splitting wrapper — registers under the "mssql"
// scheme in internal/migrator/godriver.go.
package mssqlengine

import (
	"database/sql"

	"github.com/dbtools/dbtools/internal/dbconn"
	"github.com/dbtools/dbtools/internal/ddlcheck"
	"github.com/dbtools/dbtools/internal/engine"
	"github.com/dbtools/dbtools/internal/generate"
	"github.com/dbtools/dbtools/internal/ledger"
	"github.com/dbtools/dbtools/internal/migrator"
)

func init() {
	engine.Register(MSSQL{})
}

// MSSQL is the MSSQL engine.
type MSSQL struct{}

func (MSSQL) Name() string { return "mssql" }

func (MSSQL) Open(rawURL string) (*sql.DB, error) { return dbconn.Open(rawURL) }

func (MSSQL) DDL() engine.DDLDialect { return ddl{} }

func (MSSQL) Ledger() engine.LedgerStore { return ledgerStore{} }

func (MSSQL) Introspect(db *sql.DB, excludeList []string) ([]generate.TableSchema, []string, error) {
	return generate.Introspect(db, excludeList)
}

type ddl struct{}

func (ddl) ExtractObjects(sqlText string) []ddlcheck.ObjectRef {
	return ddlcheck.ExtractObjects(sqlText)
}

func (ddl) ExtractDroppedObjects(sqlText string) []ddlcheck.ObjectRef {
	return ddlcheck.ExtractDroppedObjects(sqlText)
}

func (ddl) Exists(db *sql.DB, ref ddlcheck.ObjectRef) (bool, error) {
	return ddlcheck.Exists(db, ref)
}

type ledgerStore struct{}

func (ledgerStore) Sync(db *sql.DB, m *migrator.Migrator, migrationsDir string) error {
	return ledger.Sync(db, m, migrationsDir)
}

func (ledgerStore) SetStatus(db ledger.DBTX, version uint64, status ledger.Status, note string) error {
	return ledger.SetStatus(db, version, status, note)
}

func (ledgerStore) SetStatusWithHash(db ledger.DBTX, version uint64, status ledger.Status, note, contentHash string) error {
	return ledger.SetStatusWithHash(db, version, status, note, contentHash)
}

func (ledgerStore) List(db ledger.DBTX) ([]ledger.Entry, error) {
	return ledger.List(db)
}

func (ledgerStore) AppliedVersions(db ledger.DBTX) ([]uint64, error) {
	return ledger.AppliedVersions(db)
}
