package migrator

import (
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4/database"
	"github.com/lib/pq"
)

// fakeResetInner records what Run received so the wrapper's prepend
// behaviour is testable without a live postgres.
type fakeResetInner struct {
	lastMigration string
	runErr        error
}

func (f *fakeResetInner) Open(string) (database.Driver, error) { return f, nil }
func (f *fakeResetInner) Close() error                         { return nil }
func (f *fakeResetInner) Lock() error                          { return nil }
func (f *fakeResetInner) Unlock() error                        { return nil }
func (f *fakeResetInner) SetVersion(int, bool) error           { return nil }
func (f *fakeResetInner) Version() (int, bool, error)          { return 0, false, nil }
func (f *fakeResetInner) Drop() error                          { return nil }
func (f *fakeResetInner) Run(r io.Reader) error {
	raw, _ := io.ReadAll(r)
	f.lastMigration = string(raw)
	return f.runErr
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

// TestPgResetDriver_PrependsSessionReset is the R6/C5 regression: every
// migration must run with a clean search_path/client_min_messages, so a
// pg_dump baseline's session-scoped SETs can't poison later migrations.
func TestPgResetDriver_PrependsSessionReset(t *testing.T) {
	inner := &fakeResetInner{}
	d := &pgResetDriver{inner: inner}

	if err := d.Run(strings.NewReader("CREATE TABLE t (id int);")); err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}
	if !strings.HasPrefix(inner.lastMigration, "SET search_path TO public; RESET client_min_messages;") {
		t.Fatalf("Run() did not prepend the session reset; got %q", inner.lastMigration)
	}
	if !strings.Contains(inner.lastMigration, "CREATE TABLE t (id int);") {
		t.Fatalf("Run() dropped the migration body; got %q", inner.lastMigration)
	}
}

// TestPgResetDriver_DelegatesTheRest: the wrapper must pass through the
// driver bookkeeping untouched.
func TestPgResetDriver_DelegatesTheRest(t *testing.T) {
	inner := &fakeResetInner{}
	d := &pgResetDriver{inner: inner}

	if err := d.SetVersion(3, true); err != nil {
		t.Errorf("SetVersion() returned error: %v", err)
	}
	v, dirty, err := d.Version()
	if err != nil || v != 0 || dirty {
		t.Errorf("Version() = (%d, %v, %v), want (0, false, nil)", v, dirty, err)
	}
	if err := d.Close(); err != nil {
		t.Errorf("Close() returned error: %v", err)
	}
	if err := d.Lock(); err != nil {
		t.Errorf("Lock() returned error: %v", err)
	}
	if err := d.Unlock(); err != nil {
		t.Errorf("Unlock() returned error: %v", err)
	}
	if err := d.Drop(); err != nil {
		t.Errorf("Drop() returned error: %v", err)
	}
}

func TestOpenPostgresResetDriver_BadURL(t *testing.T) {
	_, err := openPostgresResetDriver("postgres://invalid:port:is:bad")
	if err == nil {
		t.Fatalf("openPostgresResetDriver() expected error for invalid URL, got nil")
	}
}

func TestPgResetDriver_Run_ReadError(t *testing.T) {
	inner := &fakeResetInner{}
	d := &pgResetDriver{inner: inner}

	err := d.Run(errReader{})
	if err == nil {
		t.Fatal("expected error for failing reader, got nil")
	}
	if !strings.Contains(err.Error(), "reading migration: read failed") {
		t.Errorf("expected reading migration error, got %v", err)
	}
}

func TestPgResetDriver_Run_SyntaxErrorFormatting(t *testing.T) {
	// Prefix is "SET search_path TO public; RESET client_min_messages;\n" (54 runes)
	// Position 62 in total stream = 62 - 54 = 8 in migration text
	// Migration: "CREATE TABEL foo (id int);"
	// Line 1, Col 8 is "T" of TABEL
	inner := &fakeResetInner{
		runErr: &pq.Error{
			Code:     "42601",
			Message:  `syntax error at or near "TABEL"`,
			Position: "62",
		},
	}
	d := &pgResetDriver{inner: inner}

	sqlText := "CREATE TABEL foo (id int);"
	err := d.Run(strings.NewReader(sqlText))
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "migration error at line 1, column 8:") {
		t.Errorf("expected position line/col in error, got:\n%s", errMsg)
	}
	if !strings.Contains(errMsg, "CREATE TABEL foo (id int);") {
		t.Errorf("expected snippet in error, got:\n%s", errMsg)
	}
	if !strings.Contains(errMsg, "pq: syntax error at or near \"TABEL\" (SQLSTATE 42601)") {
		t.Errorf("expected SQLSTATE 42601 in error, got:\n%s", errMsg)
	}
}

func TestPgResetDriver_Run_PermissionDiagnosticAttached(t *testing.T) {
	db := getMockDB(t)

	mockDriverInstance.mu.Lock()
	mockDriverInstance.handlers = map[string]func(args []driver.Value) (driver.Rows, error){
		"SELECT current_user": func(args []driver.Value) (driver.Rows, error) {
			return &mockRows{
				cols: []string{"current_user", "session_user", "current_database", "current_schema"},
				values: [][]driver.Value{
					{"app_user", "app_user", "testdb", "public"},
				},
			}, nil
		},
		"nspowner": func(args []driver.Value) (driver.Rows, error) {
			return &mockRows{
				cols: []string{"coalesce"},
				values: [][]driver.Value{
					{"postgres"},
				},
			}, nil
		},
		"has_schema_privilege": func(args []driver.Value) (driver.Rows, error) {
			return &mockRows{
				cols: []string{"has_usage", "has_create"},
				values: [][]driver.Value{
					{true, false},
				},
			}, nil
		},
		"azure_pg_admin": func(args []driver.Value) (driver.Rows, error) {
			return &mockRows{
				cols: []string{"exists"},
				values: [][]driver.Value{
					{false},
				},
			}, nil
		},
	}
	mockDriverInstance.mu.Unlock()

	inner := &fakeResetInner{
		runErr: &pq.Error{
			Code:    "42501",
			Message: "permission denied for schema public",
		},
	}
	d := &pgResetDriver{inner: inner, db: db}

	err := d.Run(strings.NewReader("CREATE TABLE foo (id int);"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "[permission diagnostic (SQLSTATE 42501)]") {
		t.Errorf("expected permission diagnostic header in error, got:\n%s", errMsg)
	}
	if !strings.Contains(errMsg, "schema privileges: USAGE=true, CREATE=false") {
		t.Errorf("expected schema privileges in error, got:\n%s", errMsg)
	}
	if !strings.Contains(errMsg, `remediation: User "app_user" lacks CREATE/USAGE privilege on schema "public". Run: GRANT USAGE, CREATE ON SCHEMA "public" TO "app_user";`) {
		t.Errorf("expected remediation in error, got:\n%s", errMsg)
	}
}

func TestPgResetDriver_Run_PermissionDiagnosticNoDB(t *testing.T) {
	inner := &fakeResetInner{
		runErr: &pq.Error{
			Code:    "42501",
			Message: "permission denied for schema public",
		},
	}
	d := &pgResetDriver{inner: inner, db: nil}

	err := d.Run(strings.NewReader("CREATE TABLE foo (id int);"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "pq: permission denied for schema public (SQLSTATE 42501)") {
		t.Errorf("expected pq error in message, got:\n%s", errMsg)
	}
	// With nil DB, diagnostic block is empty so no diagnostic header should be attached
	if strings.Contains(errMsg, "[permission diagnostic") {
		t.Errorf("did not expect diagnostic block when db is nil, got:\n%s", errMsg)
	}
}

func TestPgResetDriver_Run_GenericError(t *testing.T) {
	inner := &fakeResetInner{
		runErr: errors.New("connection reset by peer"),
	}
	d := &pgResetDriver{inner: inner}

	err := d.Run(strings.NewReader("CREATE TABLE foo (id int);"))
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "running migration: connection reset by peer") {
		t.Errorf("expected generic error wrapped, got %v", err)
	}
}
