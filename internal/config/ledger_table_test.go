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

// Load applies the ledger-table default, but a Config built as a struct
// literal does not go through Load — so every caller reads the table
// through this accessor rather than the raw field, or an empty table name
// would reach SQL.
func TestLedgerTableName_DefaultsForInMemoryConfig(t *testing.T) {
	var cfg config.Config
	if got := cfg.LedgerTableName(); got != config.DefaultLedgerTable {
		t.Errorf("LedgerTableName() on a zero Config = %q, want %q", got, config.DefaultLedgerTable)
	}
	cfg.Ledger.Table = "custom_history"
	if got := cfg.LedgerTableName(); got != "custom_history" {
		t.Errorf("LedgerTableName() = %q, want the configured value", got)
	}
}
