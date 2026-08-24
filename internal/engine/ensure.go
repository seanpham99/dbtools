package engine

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/lib/pq"
	_ "github.com/microsoft/go-mssqldb"
)

// EnsureDatabase creates the database specified in rawURL if it does not
// already exist. It only acts when the database instance is reachable via its
// administrative database (e.g. "postgres" on PostgreSQL or "master" on MSSQL).
// For SQLite, it ensures the parent directory exists on disk.
func EnsureDatabase(eng Engine, rawURL string) error {
	if eng == nil {
		var err error
		eng, err = ForURL(rawURL)
		if err != nil {
			return err
		}
	}

	switch eng.Name() {
	case "sqlite":
		return ensureSQLitePath(rawURL)
	case "postgres":
		return ensurePostgresDatabase(rawURL)
	case "mssql":
		return ensureMSSQLDatabase(rawURL)
	default:
		return nil
	}
}

func ensureSQLitePath(rawURL string) error {
	path := strings.TrimPrefix(rawURL, "sqlite://")
	if path == "" || path == ":memory:" {
		return nil
	}
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating sqlite directory %s: %w", dir, err)
		}
	}
	return nil
}

func ensurePostgresDatabase(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" || dbName == "postgres" {
		return nil
	}

	// First test if target DB already exists and is reachable
	testDB, err := sql.Open("postgres", rawURL)
	if err == nil {
		if pingErr := testDB.Ping(); pingErr == nil {
			testDB.Close()
			return nil
		}
		testDB.Close()
	}

	// Connect to default maintenance database "postgres"
	mURL := *u
	mURL.Path = "/postgres"
	mainDB, err := sql.Open("postgres", mURL.String())
	if err != nil {
		return nil
	}
	defer mainDB.Close()

	if err := mainDB.Ping(); err != nil {
		return nil
	}

	var exists bool
	_ = mainDB.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbName).Scan(&exists)
	if !exists {
		safeName := strings.ReplaceAll(dbName, `"`, `""`)
		_, _ = mainDB.Exec(fmt.Sprintf(`CREATE DATABASE "%s"`, safeName))
	}
	return nil
}

func ensureMSSQLDatabase(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	q := u.Query()
	dbName := q.Get("database")
	if dbName == "" || strings.EqualFold(dbName, "master") {
		return nil
	}

	sqlserverURL := *u
	sqlserverURL.Scheme = "sqlserver"

	// Test if target DB is already accessible
	testDB, err := sql.Open("sqlserver", sqlserverURL.String())
	if err == nil {
		if pingErr := testDB.Ping(); pingErr == nil {
			testDB.Close()
			return nil
		}
		testDB.Close()
	}

	// Connect to master database
	mURL := *u
	mURL.Scheme = "sqlserver"
	mq := mURL.Query()
	mq.Set("database", "master")
	mURL.RawQuery = mq.Encode()

	mainDB, err := sql.Open("sqlserver", mURL.String())
	if err != nil {
		return nil
	}
	defer mainDB.Close()

	if err := mainDB.Ping(); err != nil {
		return nil
	}

	safeName := strings.ReplaceAll(dbName, "]", "]]")
	safeLiteral := strings.ReplaceAll(dbName, "'", "''")
	query := fmt.Sprintf("IF DB_ID(N'%s') IS NULL CREATE DATABASE [%s]", safeLiteral, safeName)
	_, _ = mainDB.Exec(query)
	return nil
}
