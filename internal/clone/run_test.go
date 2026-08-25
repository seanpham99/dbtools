package clone

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/config"
	_ "github.com/seanpham99/dbtools/internal/engine/sqliteengine"
)

func setupCloneTargets(t *testing.T) (*config.Config, string, string) {
	t.Helper()
	dir := t.TempDir()
	sourceURL := "sqlite://" + filepath.Join(dir, "source.db")
	destURL := "sqlite://" + filepath.Join(dir, "dest.db")
	t.Setenv("DBTOOLS_CLONE_TEST_SOURCE_URL", sourceURL)
	t.Setenv("DBTOOLS_CLONE_TEST_DEST_URL", destURL)

	schema := `CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT NOT NULL, email TEXT NOT NULL);`
	for _, url := range []string{sourceURL, destURL} {
		path := PathFromSQLiteURL(url)
		db, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		db.Close()
		conn, err := sqliteOpenForTest(url)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(schema); err != nil {
			t.Fatal(err)
		}
		conn.Close()
	}

	cfg := &config.Config{
		Targets: map[string]config.Target{
			"prod": {URLEnv: "DBTOOLS_CLONE_TEST_SOURCE_URL"},
			"dev":  {URLEnv: "DBTOOLS_CLONE_TEST_DEST_URL"},
		},
	}
	return cfg, "prod", "dev"
}

func TestRun_CopiesRowsAndMasksEmailByDefault(t *testing.T) {
	cfg, source, dest := setupCloneTargets(t)

	sourceConn, err := sqliteOpenForTest(mustResolveURL(t, cfg, source))
	if err != nil {
		t.Fatal(err)
	}
	defer sourceConn.Close()
	if _, err := sourceConn.Exec(`INSERT INTO customers (id, name, email) VALUES (1, 'Ada', 'ada@example.com'), (2, 'Bo', 'bo@example.com')`); err != nil {
		t.Fatal(err)
	}

	result, err := Run(cfg, source, dest, Options{Mask: true})
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}
	if len(result.Tables) != 1 || result.Tables[0].Table != "customers" || result.Tables[0].RowsCopied != 2 {
		t.Fatalf("Run() result = %+v, want one customers table with 2 rows copied", result.Tables)
	}
	if len(result.Tables[0].MaskedColumns) != 1 || result.Tables[0].MaskedColumns[0] != "email" {
		t.Fatalf("Run() masked columns = %v, want [email]", result.Tables[0].MaskedColumns)
	}

	destConn, err := sqliteOpenForTest(mustResolveURL(t, cfg, dest))
	if err != nil {
		t.Fatal(err)
	}
	defer destConn.Close()
	var name, email string
	if err := destConn.QueryRow(`SELECT name, email FROM customers WHERE id = 1`).Scan(&name, &email); err != nil {
		t.Fatal(err)
	}
	if name != "Ada" {
		t.Errorf("name = %q, want Ada (non-sensitive columns copy verbatim)", name)
	}
	if email == "ada@example.com" {
		t.Error("email was not masked")
	}
}

func TestRun_NoMaskCopiesRealValues(t *testing.T) {
	cfg, source, dest := setupCloneTargets(t)
	sourceConn, err := sqliteOpenForTest(mustResolveURL(t, cfg, source))
	if err != nil {
		t.Fatal(err)
	}
	defer sourceConn.Close()
	if _, err := sourceConn.Exec(`INSERT INTO customers (id, name, email) VALUES (1, 'Ada', 'ada@example.com')`); err != nil {
		t.Fatal(err)
	}

	if _, err := Run(cfg, source, dest, Options{Mask: false}); err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	destConn, err := sqliteOpenForTest(mustResolveURL(t, cfg, dest))
	if err != nil {
		t.Fatal(err)
	}
	defer destConn.Close()
	var email string
	if err := destConn.QueryRow(`SELECT email FROM customers WHERE id = 1`).Scan(&email); err != nil {
		t.Fatal(err)
	}
	if email != "ada@example.com" {
		t.Errorf("email = %q, want unmasked ada@example.com with --no-mask", email)
	}
}

func TestRun_ClearsDestBeforeCopying(t *testing.T) {
	cfg, source, dest := setupCloneTargets(t)
	destConn, err := sqliteOpenForTest(mustResolveURL(t, cfg, dest))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := destConn.Exec(`INSERT INTO customers (id, name, email) VALUES (99, 'Stale', 'stale@example.com')`); err != nil {
		t.Fatal(err)
	}
	destConn.Close()

	if _, err := Run(cfg, source, dest, Options{Mask: false}); err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	destConn, err = sqliteOpenForTest(mustResolveURL(t, cfg, dest))
	if err != nil {
		t.Fatal(err)
	}
	defer destConn.Close()
	var count int
	if err := destConn.QueryRow(`SELECT COUNT(*) FROM customers WHERE id = 99`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Error("stale dest row survived clone; Run() must clear each table before copying")
	}
}

func TestRun_RejectsSameSourceAndDest(t *testing.T) {
	cfg, source, _ := setupCloneTargets(t)
	if _, err := Run(cfg, source, source, Options{Mask: true}); err == nil {
		t.Fatal("Run() with identical source and dest should error")
	}
}

func TestRun_RejectsEngineMismatch(t *testing.T) {
	cfg, source, dest := setupCloneTargets(t)
	destTarget := cfg.Targets[dest]
	destTarget.Engine = "postgres"
	cfg.Targets[dest] = destTarget
	if _, err := Run(cfg, source, dest, Options{Mask: true}); err == nil {
		t.Fatal("Run() with mismatched engines should error")
	}
}

func mustResolveURL(t *testing.T, cfg *config.Config, target string) string {
	t.Helper()
	url, err := cfg.ResolveURL(target)
	if err != nil {
		t.Fatal(err)
	}
	return url
}

func PathFromSQLiteURL(rawURL string) string {
	return strings.TrimPrefix(rawURL, "sqlite://")
}

func sqliteOpenForTest(rawURL string) (*sql.DB, error) {
	return sql.Open("sqlite", PathFromSQLiteURL(rawURL))
}
