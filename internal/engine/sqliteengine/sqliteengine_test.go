package sqliteengine

import (
	"database/sql"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/seanpham99/dbtools/internal/ddlcheck"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/ledger"
)

func openTemp(t *testing.T) *sql.DB {
	t.Helper()
	db, err := SQLite{}.Open("sqlite://" + filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open() returned error: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestPathFromURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"sqlite://relative/to.db", "relative/to.db"},
		{"sqlite:///abs/to.db", "/abs/to.db"},
		{"sqlite://file.db?cache=shared", "file.db"},
	}
	for _, c := range cases {
		if got := PathFromURL(c.in); got != c.want {
			t.Errorf("PathFromURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRegistryRoutesSQLiteScheme(t *testing.T) {
	eng, err := engine.ForTarget("", "sqlite://some/file.db")
	if err != nil {
		t.Fatalf("ForTarget() returned error: %v", err)
	}
	if eng.Name() != "sqlite" {
		t.Fatalf("engine name = %q, want sqlite", eng.Name())
	}

	if _, err := engine.ForTarget("sqlite", "postgres://h/db"); err == nil {
		t.Fatal("ForTarget(sqlite, postgres URL) succeeded, want scheme mismatch error")
	}
}

func TestOpenCreatesAndQueries(t *testing.T) {
	db := openTemp(t)
	if _, err := db.Exec(`CREATE TABLE t (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatalf("CREATE TABLE returned error: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO t (id, name) VALUES (1, 'a')`); err != nil {
		t.Fatalf("INSERT returned error: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM t`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("COUNT = %d, err = %v; want 1", n, err)
	}
}

func TestMapSQLiteToPython(t *testing.T) {
	cases := []struct {
		in        string
		want      string
		wantKnown bool
	}{
		{"INTEGER", "int", true},
		{"BIGINT", "int", true},
		{"TEXT", "str", true},
		{"VARCHAR(80)", "str", true},
		{"REAL", "float", true},
		{"DOUBLE PRECISION", "float", true},
		{"NUMERIC", "Decimal", true},
		{"DECIMAL(10,2)", "Decimal", true},
		{"BOOLEAN", "bool", true},
		{"DATETIME", "datetime", true},
		{"TIMESTAMP", "datetime", true},
		{"TIME", "time", true},
		{"BLOB", "bytes", true},
		{"", "bytes", true},
		{"UUID", "UUID", true},
		{"GEOMETRY", "Any", false},
	}
	for _, c := range cases {
		got, known := MapSQLiteToPython(c.in)
		if got != c.want || known != c.wantKnown {
			t.Errorf("MapSQLiteToPython(%q) = (%q, %v), want (%q, %v)", c.in, got, known, c.want, c.wantKnown)
		}
	}
}

func TestExtractObjects(t *testing.T) {
	sqlText := `
CREATE TABLE users (id INTEGER PRIMARY KEY);
CREATE TABLE IF NOT EXISTS "orders" (id INTEGER);
CREATE TEMP TABLE scratch (x INTEGER);
CREATE VIEW main.active_users AS SELECT * FROM users;
CREATE INDEX idx_users ON users(id);
CREATE TRIGGER trg AFTER INSERT ON users BEGIN SELECT 1; END;
`
	got := ddl{}.ExtractObjects(sqlText)
	want := []ddlcheck.ObjectRef{
		{Schema: "main", Name: "users", Kind: "table"},
		{Schema: "main", Name: "orders", Kind: "table"},
		{Schema: "main", Name: "scratch", Kind: "table"},
		{Schema: "main", Name: "active_users", Kind: "view"},
	}
	if len(got) != len(want) {
		t.Fatalf("ExtractObjects() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("object[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestExtractDroppedObjects(t *testing.T) {
	got := ddl{}.ExtractDroppedObjects(`DROP TABLE IF EXISTS users;
DROP VIEW active_users;
DROP INDEX idx_users;`)
	want := []ddlcheck.ObjectRef{
		{Schema: "main", Name: "users", Kind: "table"},
		{Schema: "main", Name: "active_users", Kind: "view"},
	}
	if len(got) != len(want) {
		t.Fatalf("ExtractDroppedObjects() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("object[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestExists(t *testing.T) {
	db := openTemp(t)
	if _, err := db.Exec(`CREATE TABLE t (id INTEGER); CREATE VIEW v AS SELECT * FROM t;`); err != nil {
		t.Fatalf("setup DDL returned error: %v", err)
	}

	cases := []struct {
		ref  ddlcheck.ObjectRef
		want bool
	}{
		{ddlcheck.ObjectRef{Schema: "main", Name: "t", Kind: "table"}, true},
		{ddlcheck.ObjectRef{Schema: "main", Name: "v", Kind: "view"}, true},
		{ddlcheck.ObjectRef{Schema: "main", Name: "t", Kind: "view"}, false},
		{ddlcheck.ObjectRef{Schema: "main", Name: "missing", Kind: "table"}, false},
	}
	for _, c := range cases {
		got, err := ddl{}.Exists(db, c.ref)
		if err != nil {
			t.Fatalf("Exists(%v) returned error: %v", c.ref, err)
		}
		if got != c.want {
			t.Errorf("Exists(%v) = %v, want %v", c.ref, got, c.want)
		}
	}
	if _, err := (ddl{}).Exists(db, ddlcheck.ObjectRef{Kind: "procedure"}); err == nil {
		t.Error("Exists(procedure) succeeded, want unknown-kind error")
	}
}

func TestLedgerRoundTrip(t *testing.T) {
	db := openTemp(t)
	store := ledgerStore{}
	if err := store.ensureSchema(db); err != nil {
		t.Fatalf("ensureSchema() returned error: %v", err)
	}
	if err := store.ensureSchema(db); err != nil {
		t.Fatalf("second ensureSchema() returned error: %v", err)
	}

	if err := store.SetStatus(db, 100, ledger.StatusApplied, "first"); err != nil {
		t.Fatalf("SetStatus() returned error: %v", err)
	}
	if err := store.SetStatus(db, 100, ledger.StatusReverted, "rolled back"); err != nil {
		t.Fatalf("SetStatus() upsert returned error: %v", err)
	}
	if err := store.SetStatus(db, 200, ledger.StatusApplied, "second"); err != nil {
		t.Fatalf("SetStatus() returned error: %v", err)
	}
	if err := store.backfill(db, 300, true, []uint64{100, 200, 300, 400}); err != nil {
		t.Fatalf("backfill() returned error: %v", err)
	}

	entries, err := store.List(db)
	if err != nil {
		t.Fatalf("List() returned error: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("List() returned %d entries, want 3", len(entries))
	}
	if entries[0].Version != 100 || entries[0].Status != ledger.StatusReverted || entries[0].Note != "rolled back" {
		t.Errorf("entry[0] = %+v, want reverted 100", entries[0])
	}
	if entries[0].RecordedAt == nil || time.Since(*entries[0].RecordedAt) > time.Minute {
		t.Errorf("entry[0].RecordedAt = %v, want a recent timestamp", entries[0].RecordedAt)
	}
	if entries[2].Version != 300 || entries[2].RecordedAt != nil || !strings.Contains(entries[2].Note, "backfilled") {
		t.Errorf("entry[2] = %+v, want backfilled 300 with nil RecordedAt", entries[2])
	}

	applied, err := store.AppliedVersions(db)
	if err != nil {
		t.Fatalf("AppliedVersions() returned error: %v", err)
	}
	if len(applied) != 2 || applied[0] != 200 || applied[1] != 300 {
		t.Errorf("AppliedVersions() = %v, want [200 300]", applied)
	}
}

func TestLedgerRejectsVersionsAboveIntegerRange(t *testing.T) {
	if err := (ledgerStore{}).SetStatus(nil, math.MaxInt64+1, ledger.StatusApplied, ""); err == nil || !strings.Contains(err.Error(), "INTEGER range") {
		t.Errorf("SetStatus(MaxInt64+1) err = %v, want INTEGER range error", err)
	}
	if err := (ledgerStore{}).backfill(nil, math.MaxUint64, true, []uint64{math.MaxInt64 + 1}); err == nil || !strings.Contains(err.Error(), "INTEGER range") {
		t.Errorf("backfill(MaxInt64+1) err = %v, want INTEGER range error", err)
	}
}

func TestIntrospect(t *testing.T) {
	db := openTemp(t)
	setup := `
CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    email TEXT NOT NULL,
    balance DECIMAL(10,2),
    created_at TIMESTAMP,
    avatar BLOB,
    location GEOMETRY,
    "class" TEXT
);
CREATE TABLE skipme (id INTEGER);
CREATE TABLE dbtools_migration_history (version INTEGER PRIMARY KEY);
CREATE VIEW v AS SELECT * FROM users;
`
	if _, err := db.Exec(setup); err != nil {
		t.Fatalf("setup DDL returned error: %v", err)
	}

	tables, unmapped, err := introspect(db, []string{"skipme", "dbtools_migration_history"})
	if err != nil {
		t.Fatalf("introspect() returned error: %v", err)
	}
	if len(tables) != 1 {
		t.Fatalf("introspect() returned %d tables (%v), want 1", len(tables), tables)
	}
	tbl := tables[0]
	if tbl.Schema != "main" || tbl.Name != "users" || len(tbl.Columns) != 7 {
		t.Fatalf("table = %s.%s with %d columns, want main.users with 7", tbl.Schema, tbl.Name, len(tbl.Columns))
	}

	byName := map[string]int{}
	for i, c := range tbl.Columns {
		byName[c.Name] = i
	}
	id := tbl.Columns[byName["id"]]
	if id.PythonType != "int" || id.IsNullable {
		t.Errorf("id = %+v, want non-nullable int", id)
	}
	email := tbl.Columns[byName["email"]]
	if email.PythonType != "str" || email.IsNullable {
		t.Errorf("email = %+v, want non-nullable str", email)
	}
	balance := tbl.Columns[byName["balance"]]
	if balance.PythonType != "Decimal" || !balance.IsNullable {
		t.Errorf("balance = %+v, want nullable Decimal", balance)
	}
	if got := tbl.Columns[byName["created_at"]].PythonType; got != "datetime" {
		t.Errorf("created_at type = %q, want datetime", got)
	}
	if got := tbl.Columns[byName["avatar"]].PythonType; got != "bytes" {
		t.Errorf("avatar type = %q, want bytes", got)
	}
	if got := tbl.Columns[byName["location"]].PythonType; got != "Any" {
		t.Errorf("location type = %q, want Any", got)
	}
	if got := tbl.Columns[byName["class"]].PyName; got != "class_" {
		t.Errorf("reserved column PyName = %q, want class_", got)
	}
	if len(unmapped) != 1 || !strings.Contains(unmapped[0], "GEOMETRY") {
		t.Errorf("unmapped = %v, want exactly the GEOMETRY column", unmapped)
	}
}
