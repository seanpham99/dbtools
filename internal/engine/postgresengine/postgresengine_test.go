package postgresengine

import (
	"testing"

	"github.com/dbtools/dbtools/internal/ddlcheck"
	"github.com/dbtools/dbtools/internal/engine"
)

func TestRegisteredAndRoutedByScheme(t *testing.T) {
	eng, err := engine.ForURL("postgres://user:pw@localhost:5432/db")
	if err != nil {
		t.Fatalf("ForURL(postgres://...) returned error: %v", err)
	}
	if eng.Name() != "postgres" {
		t.Fatalf("engine name = %q, want postgres", eng.Name())
	}
}

func TestForTargetValidatesConfiguredEngine(t *testing.T) {
	if _, err := engine.ForTarget("postgres", "postgres://u:p@h/db"); err != nil {
		t.Fatalf("ForTarget(postgres, postgres://) returned error: %v", err)
	}
	if _, err := engine.ForTarget("postgres", "mssql://u:p@h?database=db"); err == nil {
		t.Fatal("ForTarget(postgres, mssql://) should fail")
	}
}

func TestOpenAcceptsPostgresURL(t *testing.T) {
	db, err := Postgres{}.Open("postgres://user:pw@localhost:5432/db?sslmode=disable")
	if err != nil {
		t.Fatalf("Open() returned error: %v", err)
	}
	db.Close()
}

func TestMapPostgresToPython(t *testing.T) {
	cases := []struct {
		dataType string
		want     string
		known    bool
	}{
		{"integer", "int", true},
		{"bigint", "int", true},
		{"double precision", "float", true},
		{"numeric", "Decimal", true},
		{"boolean", "bool", true},
		{"character varying", "str", true},
		{"text", "str", true},
		{"timestamp with time zone", "datetime", true},
		{"timestamp without time zone", "datetime", true},
		{"date", "datetime", true},
		{"time without time zone", "time", true},
		{"uuid", "UUID", true},
		{"bytea", "bytes", true},
		{"jsonb", "Any", true},
		{"tsvector", "Any", false},
	}
	for _, c := range cases {
		got, known := MapPostgresToPython(c.dataType)
		if got != c.want || known != c.known {
			t.Errorf("MapPostgresToPython(%q) = %q, %v; want %q, %v", c.dataType, got, known, c.want, c.known)
		}
	}
}

func TestExtractObjects(t *testing.T) {
	sql := `
CREATE TABLE IF NOT EXISTS users (id bigint PRIMARY KEY);
CREATE TABLE billing.invoices (id bigint);
CREATE OR REPLACE VIEW "active_users" AS SELECT * FROM users;
CREATE MATERIALIZED VIEW stats AS SELECT 1;
CREATE OR REPLACE FUNCTION public.touch_updated_at() RETURNS trigger AS $$
BEGIN
    -- CREATE TABLE decoy_inside_body (id int);
    NEW.updated_at := now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE PROCEDURE do_maintenance() LANGUAGE sql AS $body$ CREATE TABLE nope(id int); $body$;
`
	got := ddl{}.ExtractObjects(sql)
	want := []ddlcheck.ObjectRef{
		{Schema: "public", Name: "users", Kind: "table"},
		{Schema: "billing", Name: "invoices", Kind: "table"},
		{Schema: "public", Name: "active_users", Kind: "view"},
		{Schema: "public", Name: "stats", Kind: "view"},
		{Schema: "public", Name: "touch_updated_at", Kind: "function"},
		{Schema: "public", Name: "do_maintenance", Kind: "procedure"},
	}
	if len(got) != len(want) {
		t.Fatalf("ExtractObjects() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ExtractObjects()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestExtractDroppedObjects(t *testing.T) {
	sql := `
DROP TABLE IF EXISTS old_users;
DROP VIEW reporting."legacy_view";
DROP FUNCTION IF EXISTS public.touch_updated_at;
`
	got := ddl{}.ExtractDroppedObjects(sql)
	want := []ddlcheck.ObjectRef{
		{Schema: "public", Name: "old_users", Kind: "table"},
		{Schema: "reporting", Name: "legacy_view", Kind: "view"},
		{Schema: "public", Name: "touch_updated_at", Kind: "function"},
	}
	if len(got) != len(want) {
		t.Fatalf("ExtractDroppedObjects() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ExtractDroppedObjects()[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestMaskNonExecutable(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"plain dollar body", "a $$ CREATE TABLE hidden(id int); $$ b", "a                                    b"},
		{"tagged body", "a $tag$ DROP TABLE x; $tag$ b", "a                           b"},
		{"digit tag", "a $fn1$ CREATE TABLE x(id int); $fn1$ b", "a                                     b"},
		{"unterminated masks to end", "x $$ CREATE TABLE y(id int);", "x                           "},
		{"dollar tag inside string literal does not open a body", "SELECT 'a $$ b';\nCREATE TABLE t (id int);", "SELECT         ;\nCREATE TABLE t (id int);"},
		{"line comment masked", "-- CREATE TABLE nope(id int)\nCREATE TABLE real_one (id int);", "                            \nCREATE TABLE real_one (id int);"},
		{"block comment masked", "/* CREATE TABLE nope(id int) */ CREATE TABLE kept (id int);", "                                CREATE TABLE kept (id int);"},
		{"nested block comment", "/* outer /* CREATE TABLE inner(x int) */ still */ CREATE VIEW v AS SELECT 1;", "                                                  CREATE VIEW v AS SELECT 1;"},
		{"doubled quote in string", "SELECT 'it''s $$ fine';\nCREATE TABLE after_str (id int);", "SELECT                ;\nCREATE TABLE after_str (id int);"},
		{"escape string with backslash quote", `SELECT E'a\' $$ b';` + "\nCREATE TABLE after_esc (id int);", "SELECT            ;\nCREATE TABLE after_esc (id int);"},
	}
	for _, c := range cases {
		if got := maskNonExecutable(c.in); got != c.want {
			t.Errorf("%s: maskNonExecutable(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestExistsRejectsUnknownKind(t *testing.T) {
	if _, err := (ddl{}).Exists(nil, ddlcheck.ObjectRef{Kind: "index"}); err == nil {
		t.Fatal("Exists() with unknown kind should fail")
	}
}
