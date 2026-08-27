package dashboard

import (
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/config"
	_ "github.com/seanpham99/dbtools/internal/engine/mssqlengine"
	"github.com/seanpham99/dbtools/internal/statusinfo"
)

// A target whose configured engine contradicts its URL scheme must show
// as an error row without the collector ever being called (no dial).
func TestBuildRows_RejectsEngineSchemeMismatchWithoutDialing(t *testing.T) {
	t.Setenv("DBTOOLS_DASH_GUARD_URL", "mssql://sa:x@192.0.2.1:1433?database=x")
	cfg := &config.Config{
		MigrationsDir: t.TempDir(),
		Targets: map[string]config.Target{
			"local": {URLEnv: "DBTOOLS_DASH_GUARD_URL", Engine: "postgres"},
		},
	}

	called := false
	rows := BuildRows(cfg, func(url, engineName, dir, upSuffix, ledgerTable, name string) (*statusinfo.Status, error) {
		called = true
		return nil, nil
	})

	if called {
		t.Fatal("collector must not be called for a mismatched target")
	}
	if len(rows) != 1 || rows[0].Err == nil {
		t.Fatalf("expected one error row, got %+v", rows)
	}
	if !strings.Contains(rows[0].Err.Error(), "does not match") {
		t.Fatalf("row error should name the mismatch, got: %v", rows[0].Err)
	}
}
