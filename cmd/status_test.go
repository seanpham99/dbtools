package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/statusinfo"
)

func TestRenderStatusTable(t *testing.T) {
	results := []statusinfo.TargetResult{
		{
			Target: "local",
			Status: &statusinfo.Status{
				Target:         "local",
				CurrentVersion: 20260101000000,
				HasVersion:     true,
				Dirty:          false,
				Pending:        nil,
			},
		},
		{
			Target:       "prod",
			Unconfigured: true,
		},
		{
			Target: "staging",
			Status: &statusinfo.Status{
				Target:         "staging",
				CurrentVersion: 20260101000000,
				HasVersion:     true,
				Dirty:          true,
				Pending:        []string{"20260102000000_add.up.sql"},
			},
		},
	}

	out := renderStatusTable(results)

	if !strings.Contains(out, "local       up to date") {
		t.Errorf("rendered output missing local status: %s", out)
	}
	if !strings.Contains(out, "prod        [unconfigured]") {
		t.Errorf("rendered output missing unconfigured prod status: %s", out)
	}
	if !strings.Contains(out, "staging     1 pending [DIRTY]") {
		t.Errorf("rendered output missing dirty staging status: %s", out)
	}
}

// TestRunStatus_ExitsNonZeroOnConnectionFailure guards the exit-code
// contract: a target status can't be collected (here: an unregistered
// engine scheme, which fails before any network call) must exit 1, not
// print "error:" on stdout while returning success.
func TestRunStatus_ExitsNonZeroOnConnectionFailure(t *testing.T) {
	origLoadConfig := loadConfig
	t.Cleanup(func() { loadConfig = origLoadConfig })

	loadConfig = func(string) (*config.Config, error) {
		return &config.Config{
			MigrationsDir: "migrations",
			Targets:       map[string]config.Target{"local": {URLEnv: "DBTOOLS_STATUS_TEST_URL"}},
		}, nil
	}
	t.Setenv("DBTOOLS_STATUS_TEST_URL", "nosuchengine://host/db")

	err := runStatus()
	if err == nil {
		t.Fatal("runStatus() with an unreachable target returned nil error, want a non-zero exit")
	}
	var exitErr *ExitCodeError
	if !errors.As(err, &exitErr) {
		t.Fatalf("runStatus() error = %v (%T), want *ExitCodeError", err, err)
	}
	if exitErr.Code != 1 {
		t.Fatalf("runStatus() exit code = %d, want 1", exitErr.Code)
	}
}
