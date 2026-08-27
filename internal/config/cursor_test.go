package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seanpham99/dbtools/internal/config"
)

func loadCfg(t *testing.T, body string) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "dbtools.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

// Unset cursor_table keeps golang-migrate's default, so every database
// dbtools has already migrated still finds its cursor where it left it.
func TestCursorTable_DefaultsToGolangMigrate(t *testing.T) {
	cfg := loadCfg(t, "migrations_dir = \"m\"\n")
	if got := cfg.CursorTableName(); got != config.DefaultCursorTable {
		t.Errorf("CursorTableName() = %q, want %q", got, config.DefaultCursorTable)
	}
}

// Pointing the ledger at schema_migrations is the documented way to coexist
// with an incumbent tool. The cursor cannot also live there, so it moves.
func TestCursorTable_MovesAsideWhenLedgerTakesTheName(t *testing.T) {
	cfg := loadCfg(t, "migrations_dir = \"m\"\n[ledger]\ntable = \"schema_migrations\"\n")
	if got := cfg.CursorTableName(); got != config.FallbackCursorTable {
		t.Errorf("CursorTableName() = %q, want %q", got, config.FallbackCursorTable)
	}
}

func TestCursorTable_ExplicitOverrideWins(t *testing.T) {
	cfg := loadCfg(t, "migrations_dir = \"m\"\n[ledger]\ncursor_table = \"my_cursor\"\n")
	if got := cfg.CursorTableName(); got != "my_cursor" {
		t.Errorf("CursorTableName() = %q, want %q", got, "my_cursor")
	}
}

// Two differently-shaped tables cannot share one name.
func TestCursorTable_RejectsExplicitCollision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dbtools.toml")
	body := "migrations_dir = \"m\"\n[ledger]\ntable = \"same\"\ncursor_table = \"same\"\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err == nil {
		t.Fatal("Load() succeeded with ledger table == cursor table, want error")
	}
}

// A configured cursor must reach golang-migrate, and must not reach the
// database driver (lib/pq rejects unknown query parameters).
func TestResolveURL_CarriesCursorTable(t *testing.T) {
	t.Setenv("CURSOR_TEST_URL", "postgres://u:p@h:5432/db?sslmode=disable")
	cfg := loadCfg(t, "migrations_dir = \"m\"\n[ledger]\ncursor_table = \"dbtools_schema_version\"\n"+
		"[targets.local]\nurl_env = \"CURSOR_TEST_URL\"\n")
	url, err := cfg.ResolveURL("local")
	if err != nil {
		t.Fatalf("ResolveURL: %v", err)
	}
	if want := "x-migrations-table=dbtools_schema_version"; !contains(url, want) {
		t.Errorf("ResolveURL() = %q, want it to carry %q", url, want)
	}
}

// The default cursor adds nothing to the URL, so existing setups keep
// byte-identical connection strings.
func TestResolveURL_DefaultCursorLeavesURLUntouched(t *testing.T) {
	raw := "postgres://u:p@h:5432/db?sslmode=disable"
	t.Setenv("CURSOR_TEST_URL", raw)
	cfg := loadCfg(t, "migrations_dir = \"m\"\n[targets.local]\nurl_env = \"CURSOR_TEST_URL\"\n")
	url, err := cfg.ResolveURL("local")
	if err != nil {
		t.Fatalf("ResolveURL: %v", err)
	}
	if url != raw {
		t.Errorf("ResolveURL() = %q, want it unchanged (%q)", url, raw)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
