// cmd/restart_test.go
package cmd

import (
	"testing"
	"time"

	"github.com/seanpham99/dbtools/internal/config"
)

func TestRunRestartWritesLocalEnv(t *testing.T) {
	origLoadConfig := loadConfig
	origRestartContainer := restartContainer
	origWriteLocalEnv := writeLocalEnv
	t.Cleanup(func() {
		loadConfig = origLoadConfig
		restartContainer = origRestartContainer
		writeLocalEnv = origWriteLocalEnv
	})

	loadConfig = fakeLocalConfig("DBTOOLS_LOCAL_URL")
	restartContainer = func(string, string, string, time.Duration, bool) (string, error) {
		return "mssql://sa:pw@localhost:14330?database=dbtools_local", nil
	}
	var wrote map[string]string
	writeLocalEnv = func(vars map[string]string) error {
		wrote = vars
		return nil
	}

	if err := runRestart(); err != nil {
		t.Fatalf("runRestart() returned error: %v", err)
	}
	if wrote["DBTOOLS_LOCAL_URL"] != "mssql://sa:pw@localhost:14330?database=dbtools_local" {
		t.Fatalf("runRestart() wrote %#v", wrote)
	}
}

func TestRunRestartMissingLocalTarget(t *testing.T) {
	origLoadConfig := loadConfig
	t.Cleanup(func() { loadConfig = origLoadConfig })
	loadConfig = func(string) (*config.Config, error) {
		return &config.Config{MigrationsDir: "migrations", Targets: map[string]config.Target{}}, nil
	}
	if err := runRestart(); err == nil {
		t.Fatal("runRestart() with no local target configured returned nil error, want error")
	}
}

func TestRunRestartSqliteIsNoop(t *testing.T) {
	origLoadConfig := loadConfig
	t.Cleanup(func() { loadConfig = origLoadConfig })
	loadConfig = func(string) (*config.Config, error) {
		return &config.Config{Targets: map[string]config.Target{
			"local": {URLEnv: "DBTOOLS_LOCAL_URL", Engine: "sqlite"},
		}}, nil
	}
	if err := runRestart(); err != nil {
		t.Fatalf("runRestart() on sqlite target returned error: %v, want nil (no-op)", err)
	}
}
