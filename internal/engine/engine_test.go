package engine

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/generate"
)

type fakeEngine struct{ name string }

func (f fakeEngine) Name() string                 { return f.name }
func (f fakeEngine) Open(string) (*sql.DB, error) { return nil, nil }
func (f fakeEngine) DDL() DDLDialect              { return nil }
func (f fakeEngine) Ledger() LedgerStore          { return nil }
func (f fakeEngine) Introspect(*sql.DB, []string) ([]generate.TableSchema, []string, error) {
	return nil, nil, nil
}

func withFake(t *testing.T, name string) {
	t.Helper()
	Register(fakeEngine{name: name})
	t.Cleanup(func() { delete(registry, name) })
}

func TestForURL(t *testing.T) {
	withFake(t, "fakedb")

	e, err := ForURL("fakedb://user:pass@host:1433?database=x")
	if err != nil {
		t.Fatalf("ForURL() returned error: %v", err)
	}
	if e.Name() != "fakedb" {
		t.Fatalf("ForURL() resolved %q, want fakedb", e.Name())
	}

	if _, err := ForURL("nosuchengine://host"); err == nil {
		t.Fatal("ForURL() with unregistered scheme should fail")
	} else if !strings.Contains(err.Error(), "unknown engine") {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := ForURL("not-a-url-no-scheme"); err == nil {
		t.Fatal("ForURL() with schemeless URL should fail")
	}
}

func TestForURL_PostgresqlSchemeAliasesToPostgres(t *testing.T) {
	withFake(t, "postgres")

	e, err := ForURL("postgresql://user:pass@host:5432/db?sslmode=disable")
	if err != nil {
		t.Fatalf("ForURL(postgresql://...) returned error: %v", err)
	}
	if e.Name() != "postgres" {
		t.Fatalf("ForURL(postgresql://...) resolved %q, want postgres", e.Name())
	}
}

func TestForTarget_PostgresqlSchemeMatchesConfiguredPostgresEngine(t *testing.T) {
	withFake(t, "postgres")

	if _, err := ForTarget("postgres", "postgresql://user:pass@host:5432/db"); err != nil {
		t.Fatalf("ForTarget(postgres, postgresql://...) returned error: %v, want nil (postgresql is an alias for postgres)", err)
	}
}

func TestForTarget(t *testing.T) {
	withFake(t, "fakedb")

	// Empty engine name: infer from scheme.
	if _, err := ForTarget("", "fakedb://host"); err != nil {
		t.Fatalf("ForTarget(\"\") returned error: %v", err)
	}

	// Matching engine name: fine.
	if _, err := ForTarget("fakedb", "fakedb://host"); err != nil {
		t.Fatalf("ForTarget(match) returned error: %v", err)
	}

	// Mismatch between configured engine and URL scheme: refused.
	withFake(t, "otherdb")
	if _, err := ForTarget("otherdb", "fakedb://host"); err == nil {
		t.Fatal("ForTarget(mismatch) should fail")
	} else if !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegisterDuplicatePanics(t *testing.T) {
	withFake(t, "fakedb")
	defer func() {
		if recover() == nil {
			t.Fatal("Register() of a duplicate name should panic")
		}
	}()
	Register(fakeEngine{name: "fakedb"})
}
