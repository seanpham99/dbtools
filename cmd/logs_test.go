// cmd/logs_test.go
package cmd

import (
	"testing"

	"github.com/seanpham99/dbtools/internal/config"
)

func TestRunLogsCallsLogsContainer(t *testing.T) {
	origLoadConfig := loadConfig
	origLogsContainer := logsContainer
	t.Cleanup(func() {
		loadConfig = origLoadConfig
		logsContainer = origLogsContainer
	})

	loadConfig = fakeLocalConfig("DBTOOLS_LOCAL_URL")
	var gotEngine string
	logsContainer = func(engineName, projectID string, follow bool) error {
		gotEngine = engineName
		return nil
	}

	if err := runLogs(); err != nil {
		t.Fatalf("runLogs() returned error: %v", err)
	}
	if gotEngine != "mssql" {
		t.Fatalf("runLogs() called logsContainer with engine %q, want mssql", gotEngine)
	}
}

func TestRunLogsSqliteErrors(t *testing.T) {
	origLoadConfig := loadConfig
	t.Cleanup(func() { loadConfig = origLoadConfig })
	loadConfig = func(string) (*config.Config, error) {
		return &config.Config{Targets: map[string]config.Target{
			"local": {URLEnv: "DBTOOLS_LOCAL_URL", Engine: "sqlite"},
		}}, nil
	}
	if err := runLogs(); err == nil {
		t.Fatal("runLogs() on a sqlite target returned nil error, want error")
	}
}
