package dblock

import (
	"context"
	"database/sql"
	"fmt"
)

// ForEngine returns the acquire/release pair for engineName.
//
// Every engine here uses a *session*-scoped advisory lock rather than a
// transactional one, because an apply run spans many statements and files
// and, on some engines, DDL that cannot sit inside a transaction at all.
func ForEngine(engineName string) (Acquirer, Releaser, error) {
	switch engineName {
	case "postgres":
		return postgresAcquire, postgresRelease, nil
	case "mysql":
		return mysqlAcquire, mysqlRelease, nil
	case "mssql":
		return mssqlAcquire, mssqlRelease, nil
	case "sqlite":
		// SQLite serialises writers itself: a second writer gets SQLITE_BUSY
		// rather than interleaving. There is also no second process sharing a
		// server to coordinate with — the file *is* the database.
		return noopAcquire, noopRelease, nil
	default:
		return nil, nil, fmt.Errorf("no migration lock implemented for engine %q", engineName)
	}
}

func noopAcquire(context.Context, *sql.Conn, string) error { return nil }
func noopRelease(context.Context, *sql.Conn, string) error { return nil }

// pg_advisory_lock blocks until the lock is free and takes only a numeric
// key. It is refcounted per session, so every acquire needs exactly one
// release — which is why Lock.Release is idempotent.
func postgresAcquire(ctx context.Context, conn *sql.Conn, key string) error {
	_, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", NumericKey(key))
	return err
}

func postgresRelease(ctx context.Context, conn *sql.Conn, key string) error {
	// pg_advisory_unlock returns false when this session did not hold the
	// lock. That means the lock was lost or released twice, and silently
	// ignoring it would hide a bug that lets two runners proceed together.
	var released bool
	if err := conn.QueryRowContext(ctx, "SELECT pg_advisory_unlock($1)", NumericKey(key)).Scan(&released); err != nil {
		return err
	}
	if !released {
		return fmt.Errorf("advisory lock %q was not held by this session at release time", key)
	}
	return nil
}

// GET_LOCK takes a name and a timeout in seconds. A negative timeout waits
// indefinitely, which matches the blocking semantics of the other engines;
// ctx cancellation is what bounds the wait.
func mysqlAcquire(ctx context.Context, conn *sql.Conn, key string) error {
	var got sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT GET_LOCK(?, -1)", mysqlKey(key)).Scan(&got); err != nil {
		return err
	}
	// 1 = acquired, 0 = timed out, NULL = error.
	if !got.Valid || got.Int64 != 1 {
		return fmt.Errorf("could not acquire advisory lock %q", key)
	}
	return nil
}

func mysqlRelease(ctx context.Context, conn *sql.Conn, key string) error {
	var released sql.NullInt64
	if err := conn.QueryRowContext(ctx, "SELECT RELEASE_LOCK(?)", mysqlKey(key)).Scan(&released); err != nil {
		return err
	}
	if !released.Valid || released.Int64 != 1 {
		return fmt.Errorf("advisory lock %q was not held by this session at release time", key)
	}
	return nil
}

// MySQL lock names are limited to 64 characters since 5.7. Keys are short by
// construction, but a long database name could push past it, so truncate
// deterministically rather than letting the server reject the call.
func mysqlKey(key string) string {
	const maxLockName = 64
	if len(key) <= maxLockName {
		return key
	}
	return key[:maxLockName]
}

// sp_getapplock with @LockOwner='Session' outlives the calling batch, which
// is what an apply run needs. @LockTimeout=-1 waits indefinitely.
func mssqlAcquire(ctx context.Context, conn *sql.Conn, key string) error {
	var result int
	err := conn.QueryRowContext(ctx, `
		DECLARE @r INT;
		EXEC @r = sp_getapplock @Resource = @p1, @LockMode = 'Exclusive',
			@LockOwner = 'Session', @LockTimeout = -1;
		SELECT @r`, mssqlKey(key)).Scan(&result)
	if err != nil {
		return err
	}
	// 0 = granted, 1 = granted after waiting; negatives are failures.
	if result < 0 {
		return fmt.Errorf("could not acquire advisory lock %q (sp_getapplock returned %d)", key, result)
	}
	return nil
}

func mssqlRelease(ctx context.Context, conn *sql.Conn, key string) error {
	var result int
	err := conn.QueryRowContext(ctx, `
		DECLARE @r INT;
		EXEC @r = sp_releaseapplock @Resource = @p1, @LockOwner = 'Session';
		SELECT @r`, mssqlKey(key)).Scan(&result)
	if err != nil {
		return err
	}
	if result < 0 {
		return fmt.Errorf("advisory lock %q was not held by this session at release time "+
			"(sp_releaseapplock returned %d)", key, result)
	}
	return nil
}

// sp_getapplock resource names are limited to 255 characters.
func mssqlKey(key string) string {
	const maxResourceName = 255
	if len(key) <= maxResourceName {
		return key
	}
	return key[:maxResourceName]
}

// DatabaseName asks the server which database this connection is on.
//
// Deriving it from the connection URL instead would be a correctness hole:
// the same database reached through two URL forms (postgres:// vs
// postgresql://, 127.0.0.1 vs localhost, MySQL's tcp(host:port)) would
// produce two different lock keys, and two runs that must exclude each
// other would not. One round trip removes the whole class.
//
// Returns "" when it cannot be determined, which KeyFor turns into a
// global key — less concurrency, never less safety.
func DatabaseName(ctx context.Context, db *sql.DB, engineName string) string {
	var query string
	switch engineName {
	case "postgres":
		query = "SELECT current_database()"
	case "mysql":
		query = "SELECT DATABASE()"
	case "mssql":
		query = "SELECT DB_NAME()"
	default:
		return ""
	}
	var name sql.NullString
	if err := db.QueryRowContext(ctx, query).Scan(&name); err != nil || !name.Valid {
		return ""
	}
	return name.String
}
