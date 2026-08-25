package migrator

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/lib/pq"
)

// PermissionReport holds diagnostic information collected when a PostgreSQL
// migration fails with SQLSTATE 42501 (insufficient_privilege).
type PermissionReport struct {
	CurrentUser       string
	SessionUser       string
	Database          string
	Schema            string
	SchemaOwner       string
	HasUsage          bool
	HasCreate         bool
	IsAzureAdmin      bool
	HasAzureAdminRole bool
	Table             string
	Remediation       string
}

// FormatPermissionReport renders a structured diagnostic text block from a PermissionReport.
func FormatPermissionReport(report PermissionReport) string {
	var b strings.Builder
	b.WriteString("[permission diagnostic (SQLSTATE 42501)]\n")
	fmt.Fprintf(&b, "  current_user: %s\n", report.CurrentUser)
	fmt.Fprintf(&b, "  session_user: %s\n", report.SessionUser)
	fmt.Fprintf(&b, "  database: %s\n", report.Database)
	fmt.Fprintf(&b, "  schema: %s (owner: %s)\n", report.Schema, report.SchemaOwner)
	fmt.Fprintf(&b, "  schema privileges: USAGE=%t, CREATE=%t\n", report.HasUsage, report.HasCreate)
	hasAdmin := report.HasAzureAdminRole || report.IsAzureAdmin
	fmt.Fprintf(&b, "  azure_pg_admin member: %t\n", hasAdmin)
	fmt.Fprintf(&b, "  remediation: %s", report.Remediation)
	return b.String()
}

// queryOptionalString runs a single-column query and returns its value,
// or fallback if the query fails or returns NULL/empty. Diagnostic
// queries must never let a failure here mask the original migration
// error, so errors are swallowed rather than propagated.
func queryOptionalString(ctx context.Context, db *sql.DB, fallback, query string, args ...any) string {
	var s sql.NullString
	if err := db.QueryRowContext(ctx, query, args...).Scan(&s); err != nil || !s.Valid || s.String == "" {
		return fallback
	}
	return s.String
}

// queryOptionalBool runs a single-column boolean query and returns its
// value, or false if the query fails. Same swallow-errors rationale as
// queryOptionalString.
func queryOptionalBool(ctx context.Context, db *sql.DB, query string, args ...any) bool {
	var b sql.NullBool
	_ = db.QueryRowContext(ctx, query, args...).Scan(&b)
	return b.Bool
}

// RunPermissionDiagnostic inspects the database connection and queries permission
// metadata if the given error is a PostgreSQL SQLSTATE 42501 error.
// It safely recovers from any query failures so diagnostic collection never panics or masks the original failure.
func RunPermissionDiagnostic(ctx context.Context, db *sql.DB, pqErr *pq.Error) string {
	if db == nil || pqErr == nil || pqErr.Code != "42501" {
		return ""
	}

	if ctx == nil {
		ctx = context.Background()
	}

	report := PermissionReport{
		Table: pqErr.Table,
	}

	// 1. Query current user, session user, current database, and current schema
	var currentUser, sessionUser, currentDB, currentSchema sql.NullString
	row := db.QueryRowContext(ctx, "SELECT current_user, session_user, current_database(), coalesce(current_schema(), 'public')")
	if err := row.Scan(&currentUser, &sessionUser, &currentDB, &currentSchema); err != nil {
		return ""
	}

	report.CurrentUser = currentUser.String
	report.SessionUser = sessionUser.String
	report.Database = currentDB.String

	if pqErr.Schema != "" {
		report.Schema = pqErr.Schema
	} else if currentSchema.Valid && currentSchema.String != "" {
		report.Schema = currentSchema.String
	} else {
		report.Schema = "public"
	}

	// 2. Query schema owner
	report.SchemaOwner = queryOptionalString(ctx, db, "unknown",
		"SELECT coalesce(pg_get_userbyid(nspowner), 'unknown') FROM pg_namespace WHERE nspname = $1", report.Schema)

	// 3. Query schema privileges (one row, two columns — not the same
	// single-value shape as 2 and 4, so it stays a direct query rather
	// than going through queryOptionalBool).
	var hasUsage, hasCreate sql.NullBool
	_ = db.QueryRowContext(ctx, "SELECT has_schema_privilege(current_user, $1, 'USAGE'), has_schema_privilege(current_user, $1, 'CREATE')", report.Schema).Scan(&hasUsage, &hasCreate)
	report.HasUsage = hasUsage.Bool
	report.HasCreate = hasCreate.Bool

	// 4. Query azure_pg_admin role membership
	isAzureAdmin := queryOptionalBool(ctx, db,
		"SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'azure_pg_admin' AND pg_has_role(current_user, 'azure_pg_admin', 'MEMBER'))")
	report.IsAzureAdmin = isAzureAdmin
	report.HasAzureAdminRole = isAzureAdmin

	// 5. Build remediation advice
	if !report.HasUsage || !report.HasCreate {
		report.Remediation = fmt.Sprintf("User %q lacks CREATE/USAGE privilege on schema %q. Run: GRANT USAGE, CREATE ON SCHEMA %q TO %q;", report.CurrentUser, report.Schema, report.Schema, report.CurrentUser)
	} else if report.Table != "" {
		report.Remediation = fmt.Sprintf("User %q lacks permissions on table %q. Run: GRANT ALL ON TABLE %q TO %q;", report.CurrentUser, report.Table, report.Table, report.CurrentUser)
	} else {
		report.Remediation = fmt.Sprintf("User %q lacks required privileges on schema %q. Run: GRANT ALL ON SCHEMA %q TO %q;", report.CurrentUser, report.Schema, report.Schema, report.CurrentUser)
	}

	return FormatPermissionReport(report)
}
