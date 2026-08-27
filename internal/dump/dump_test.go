package dump_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/dump"
	"github.com/seanpham99/dbtools/internal/engine"
	_ "github.com/seanpham99/dbtools/internal/engine/mssqlengine"
	_ "github.com/seanpham99/dbtools/internal/engine/mysqlengine"
	_ "github.com/seanpham99/dbtools/internal/engine/postgresengine"
	_ "github.com/seanpham99/dbtools/internal/engine/sqliteengine"
)

func TestSchema_MissingToolGivesInstallHint(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // empty PATH — no tool findable
	tests := []struct {
		engine   string
		toolName string
	}{
		{"postgres", "pg_dump"},
		{"mysql", "mysqldump"},
		{"mssql", "mssql-scripter"},
	}
	for _, tt := range tests {
		t.Run(tt.engine, func(t *testing.T) {
			eng, err := engine.ForName(tt.engine)
			if err != nil {
				t.Fatal(err)
			}
			_, err = dump.Schema(eng, tt.engine+"://ignored", dump.Options{})
			if err == nil || !strings.Contains(err.Error(), tt.toolName) {
				t.Fatalf("Schema() with no %s on PATH = %v, want an error naming %s", tt.toolName, err, tt.toolName)
			}
		})
	}
}

func TestStripPostgresSessionState(t *testing.T) {
	in := "CREATE TABLE t (id int);\nSELECT pg_catalog.set_config('search_path', '', false);\nSET client_min_messages = warning;\nCREATE TABLE u (id int);\n"
	out := dump.StripPostgresSessionState(in)
	if strings.Contains(out, "set_config") || strings.Contains(out, "client_min_messages") {
		t.Errorf("StripPostgresSessionState() = %q, still contains session-state lines", out)
	}
	if !strings.Contains(out, "CREATE TABLE t") || !strings.Contains(out, "CREATE TABLE u") {
		t.Errorf("StripPostgresSessionState() = %q, lost real DDL", out)
	}
}

func TestSchema_SQLiteNeedsNoExternalTool(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // prove sqlite doesn't even look at PATH
	eng, err := engine.ForName("sqlite")
	if err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := eng.Open("sqlite://" + dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE widgets (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	sqlText, err := dump.SchemaFromDB(eng, db)
	if err != nil {
		t.Fatalf("SchemaFromDB() returned error: %v", err)
	}
	if !strings.Contains(sqlText, "CREATE TABLE widgets") {
		t.Errorf("SchemaFromDB() = %q, want it to contain the widgets table DDL", sqlText)
	}

	// Also test dump.Schema on sqlite
	sqlTextFromSchema, err := dump.Schema(eng, "sqlite://"+dbPath, dump.Options{})
	if err != nil {
		t.Fatalf("Schema() returned error: %v", err)
	}
	if !strings.Contains(sqlTextFromSchema, "CREATE TABLE widgets") {
		t.Errorf("Schema() = %q, want it to contain the widgets table DDL", sqlTextFromSchema)
	}
}

func TestSchema_SQLiteExcludesMigrationLedgerTables(t *testing.T) {
	eng, err := engine.ForName("sqlite")
	if err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := eng.Open("sqlite://" + dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
CREATE TABLE widgets (id INTEGER PRIMARY KEY);
CREATE TABLE schema_migrations (version bigint, dirty boolean);
CREATE TABLE dbtools_migration_history (version bigint);
`); err != nil {
		t.Fatal(err)
	}

	sqlText, err := dump.Schema(eng, "sqlite://"+dbPath, dump.Options{}, "schema_migrations", "dbtools_migration_history")
	if err != nil {
		t.Fatalf("Schema() returned error: %v", err)
	}
	if !strings.Contains(sqlText, "widgets") {
		t.Errorf("Schema() = %q, want it to contain widgets table", sqlText)
	}
	if strings.Contains(sqlText, "schema_migrations") || strings.Contains(sqlText, "dbtools_migration_history") {
		t.Errorf("Schema() = %q, want migration tables excluded", sqlText)
	}
}

func TestStripMSSQLUseStatement(t *testing.T) {
	in := "USE [dbtools_scratch_mssql_123];\nGO\nCREATE TABLE widgets (id INT PRIMARY KEY);\n"
	out := dump.StripMSSQLUseStatement(in)
	if strings.Contains(out, "USE [") {
		t.Errorf("StripMSSQLUseStatement() = %q, still contains USE statement", out)
	}
	if !strings.Contains(out, "CREATE TABLE widgets") {
		t.Errorf("StripMSSQLUseStatement() = %q, lost DDL", out)
	}
}
