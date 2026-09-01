package postgresengine

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/lib/pq"
)

func TestFormatPermissionReport(t *testing.T) {
	t.Run("missing create permission", func(t *testing.T) {
		report := PermissionReport{
			CurrentUser:       "app_user",
			SessionUser:       "app_user",
			Database:          "mydb",
			Schema:            "public",
			SchemaOwner:       "postgres",
			HasUsage:          true,
			HasCreate:         false,
			IsAzureAdmin:      false,
			HasAzureAdminRole: false,
			Remediation:       `User "app_user" lacks CREATE/USAGE privilege on schema "public". Run: GRANT USAGE, CREATE ON SCHEMA "public" TO "app_user";`,
		}

		got := FormatPermissionReport(report)
		want := `[permission diagnostic (SQLSTATE 42501)]
  current_user: app_user
  session_user: app_user
  database: mydb
  schema: public (owner: postgres)
  schema privileges: USAGE=true, CREATE=false
  azure_pg_admin member: false
  remediation: User "app_user" lacks CREATE/USAGE privilege on schema "public". Run: GRANT USAGE, CREATE ON SCHEMA "public" TO "app_user";`

		if got != want {
			t.Errorf("FormatPermissionReport got:\n%s\nwant:\n%s", got, want)
		}
	})

	t.Run("missing usage and azure admin true", func(t *testing.T) {
		report := PermissionReport{
			CurrentUser:       "service_role",
			SessionUser:       "azure_admin",
			Database:          "prod_db",
			Schema:            "app_schema",
			SchemaOwner:       "db_owner",
			HasUsage:          false,
			HasCreate:         false,
			IsAzureAdmin:      true,
			HasAzureAdminRole: true,
			Remediation:       `User "service_role" lacks CREATE/USAGE privilege on schema "app_schema". Run: GRANT USAGE, CREATE ON SCHEMA "app_schema" TO "service_role";`,
		}

		got := FormatPermissionReport(report)
		if !strings.Contains(got, "azure_pg_admin member: true") {
			t.Errorf("expected azure_pg_admin member: true in:\n%s", got)
		}
		if !strings.Contains(got, "schema privileges: USAGE=false, CREATE=false") {
			t.Errorf("expected schema privileges in:\n%s", got)
		}
	})

	t.Run("table permission remediation", func(t *testing.T) {
		report := PermissionReport{
			CurrentUser: "read_user",
			SessionUser: "read_user",
			Database:    "mydb",
			Schema:      "public",
			SchemaOwner: "postgres",
			HasUsage:    true,
			HasCreate:   true,
			Table:       "schema_migrations",
			Remediation: `User "read_user" lacks permissions on table "schema_migrations". Run: GRANT ALL ON TABLE "schema_migrations" TO "read_user";`,
		}

		got := FormatPermissionReport(report)
		if !strings.Contains(got, `remediation: User "read_user" lacks permissions on table "schema_migrations". Run: GRANT ALL ON TABLE "schema_migrations" TO "read_user";`) {
			t.Errorf("expected table remediation in:\n%s", got)
		}
	})
}

func TestSSLDiagnostic(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantHint bool
	}{
		{
			name:     "nil error",
			err:      nil,
			wantHint: false,
		},
		{
			name:     "non-pq error",
			err:      errors.New("some io error"),
			wantHint: false,
		},
		{
			name: "08001 with ssl message",
			err: &pq.Error{
				Code:    "08001",
				Message: "SSL is not enabled on the server",
			},
			wantHint: true,
		},
		{
			name: "error message containing ssl is not enabled without 08001",
			err: &pq.Error{
				Code:    "08P01",
				Message: "server error: SSL is not enabled on the server",
			},
			wantHint: true,
		},
		{
			name: "other pq error",
			err: &pq.Error{
				Code:    "42P01",
				Message: "relation foo does not exist",
			},
			wantHint: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SSLDiagnostic(tt.err)
			if tt.wantHint {
				if got == "" {
					t.Fatalf("SSLDiagnostic(%v) = empty, want hint", tt.err)
				}
				if !strings.Contains(got, "sslmode=disable") {
					t.Errorf("SSLDiagnostic(%v) = %q, want it to contain sslmode=disable", tt.err, got)
				}
			} else {
				if got != "" {
					t.Fatalf("SSLDiagnostic(%v) = %q, want empty", tt.err, got)
				}
			}
		})
	}
}

// mockDriver implements a lightweight driver.Driver for testing RunPermissionDiagnostic.
type mockDriver struct {
	mu       sync.Mutex
	handlers map[string]func(args []driver.Value) (driver.Rows, error)
}

func (m *mockDriver) Open(name string) (driver.Conn, error) {
	return &mockConn{driver: m}, nil
}

type mockConn struct {
	driver *mockDriver
}

func (c *mockConn) Prepare(query string) (driver.Stmt, error) {
	return &mockStmt{conn: c, query: query}, nil
}

func (c *mockConn) Close() error              { return nil }
func (c *mockConn) Begin() (driver.Tx, error) { return nil, errors.New("not supported") }

type mockStmt struct {
	conn  *mockConn
	query string
}

func (s *mockStmt) Close() error { return nil }
func (s *mockStmt) NumInput() int {
	return -1
}

func (s *mockStmt) Exec(args []driver.Value) (driver.Result, error) {
	return nil, errors.New("not implemented")
}

func (s *mockStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.conn.driver.mu.Lock()
	defer s.conn.driver.mu.Unlock()

	for q, h := range s.conn.driver.handlers {
		if strings.Contains(s.query, q) {
			return h(args)
		}
	}
	return nil, errors.New("no mock handler for query: " + s.query)
}

type mockRows struct {
	cols   []string
	values [][]driver.Value
	pos    int
}

func (r *mockRows) Columns() []string { return r.cols }
func (r *mockRows) Close() error      { return nil }
func (r *mockRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.pos])
	r.pos++
	return nil
}

var (
	mockDriverInstance = &mockDriver{
		handlers: make(map[string]func(args []driver.Value) (driver.Rows, error)),
	}
	mockInitOnce sync.Once
)

func getMockDB(t *testing.T) *sql.DB {
	mockInitOnce.Do(func() {
		sql.Register("pgdiagnostic_mock", mockDriverInstance)
	})
	db, err := sql.Open("pgdiagnostic_mock", "")
	if err != nil {
		t.Fatalf("failed to open mock db: %v", err)
	}
	return db
}

func TestRunPermissionDiagnostic(t *testing.T) {
	ctx := context.Background()

	t.Run("nil db", func(t *testing.T) {
		pqErr := &pq.Error{Code: "42501"}
		got := RunPermissionDiagnostic(ctx, nil, pqErr)
		if got != "" {
			t.Errorf("expected empty string for nil db, got %q", got)
		}
	})

	t.Run("nil pqErr", func(t *testing.T) {
		db := getMockDB(t)
		got := RunPermissionDiagnostic(ctx, db, nil)
		if got != "" {
			t.Errorf("expected empty string for nil pqErr, got %q", got)
		}
	})

	t.Run("non-42501 error code", func(t *testing.T) {
		db := getMockDB(t)
		pqErr := &pq.Error{Code: "42P01", Message: "table not found"}
		got := RunPermissionDiagnostic(ctx, db, pqErr)
		if got != "" {
			t.Errorf("expected empty string for 42P01 error, got %q", got)
		}
	})

	t.Run("successful diagnostic run with schema permission failure", func(t *testing.T) {
		db := getMockDB(t)

		mockDriverInstance.mu.Lock()
		mockDriverInstance.handlers = map[string]func(args []driver.Value) (driver.Rows, error){
			"SELECT current_user": func(args []driver.Value) (driver.Rows, error) {
				return &mockRows{
					cols: []string{"current_user", "session_user", "current_database", "current_schema"},
					values: [][]driver.Value{
						{"app_user", "app_user", "testdb", "custom_schema"},
					},
				}, nil
			},
			"nspowner": func(args []driver.Value) (driver.Rows, error) {
				return &mockRows{
					cols: []string{"coalesce"},
					values: [][]driver.Value{
						{"admin_role"},
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

		pqErr := &pq.Error{
			Code:    "42501",
			Message: "permission denied for schema custom_schema",
		}

		got := RunPermissionDiagnostic(ctx, db, pqErr)
		if got == "" {
			t.Fatal("expected non-empty diagnostic output, got empty")
		}

		if !strings.Contains(got, "[permission diagnostic (SQLSTATE 42501)]") {
			t.Errorf("missing header in: %s", got)
		}
		if !strings.Contains(got, "current_user: app_user") {
			t.Errorf("missing current_user in: %s", got)
		}
		if !strings.Contains(got, "schema: custom_schema (owner: admin_role)") {
			t.Errorf("missing schema/owner in: %s", got)
		}
		if !strings.Contains(got, "schema privileges: USAGE=true, CREATE=false") {
			t.Errorf("missing privileges in: %s", got)
		}
		if !strings.Contains(got, "azure_pg_admin member: false") {
			t.Errorf("missing azure_pg_admin in: %s", got)
		}
		if !strings.Contains(got, `remediation: User "app_user" lacks CREATE/USAGE privilege on schema "custom_schema". Run: GRANT USAGE, CREATE ON SCHEMA "custom_schema" TO "app_user";`) {
			t.Errorf("missing remediation in: %s", got)
		}
	})

	t.Run("table permission remediation when schema privileges are present", func(t *testing.T) {
		db := getMockDB(t)

		mockDriverInstance.mu.Lock()
		mockDriverInstance.handlers = map[string]func(args []driver.Value) (driver.Rows, error){
			"SELECT current_user": func(args []driver.Value) (driver.Rows, error) {
				return &mockRows{
					cols: []string{"current_user", "session_user", "current_database", "current_schema"},
					values: [][]driver.Value{
						{"readonly_user", "readonly_user", "testdb", "public"},
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
						{true, true},
					},
				}, nil
			},
			"azure_pg_admin": func(args []driver.Value) (driver.Rows, error) {
				return &mockRows{
					cols: []string{"exists"},
					values: [][]driver.Value{
						{true},
					},
				}, nil
			},
		}
		mockDriverInstance.mu.Unlock()

		pqErr := &pq.Error{
			Code:    "42501",
			Message: "permission denied for table schema_migrations",
			Table:   "schema_migrations",
			Schema:  "public",
		}

		got := RunPermissionDiagnostic(ctx, db, pqErr)
		if got == "" {
			t.Fatal("expected non-empty diagnostic output, got empty")
		}

		if !strings.Contains(got, "azure_pg_admin member: true") {
			t.Errorf("missing azure_pg_admin member: true in: %s", got)
		}
		if !strings.Contains(got, `remediation: User "readonly_user" lacks permissions on table "schema_migrations". Run: GRANT ALL ON TABLE "schema_migrations" TO "readonly_user";`) {
			t.Errorf("missing table remediation in: %s", got)
		}
	})

	t.Run("query failure gracefully handled", func(t *testing.T) {
		db := getMockDB(t)

		mockDriverInstance.mu.Lock()
		mockDriverInstance.handlers = map[string]func(args []driver.Value) (driver.Rows, error){
			"SELECT current_user": func(args []driver.Value) (driver.Rows, error) {
				return nil, errors.New("db disconnected")
			},
		}
		mockDriverInstance.mu.Unlock()

		pqErr := &pq.Error{
			Code:    "42501",
			Message: "permission denied",
		}

		got := RunPermissionDiagnostic(ctx, db, pqErr)
		if got != "" {
			t.Errorf("expected empty string on connection failure, got %q", got)
		}
	})
}
