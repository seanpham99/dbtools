package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/localenv"
)

func fakeLocalConfig(urlEnv string) func(string) (*config.Config, error) {
	return func(string) (*config.Config, error) {
		return &config.Config{
			MigrationsDir: "migrations",
			Targets:       map[string]config.Target{"local": {URLEnv: urlEnv}},
		}, nil
	}
}

func TestLoadLocalEnvIntoProcessEnvironment(t *testing.T) {
	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() returned error: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("os.Chdir() returned error: %v", err)
	}

	if err := os.MkdirAll(localenv.Dir, 0o755); err != nil {
		t.Fatalf("os.MkdirAll() returned error: %v", err)
	}
	if err := os.WriteFile(localenv.Path(), []byte("DBTOOLS_LOCAL_URL=mssql://sa:pw@localhost:14330?database=dbtools_local\n"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() returned error: %v", err)
	}

	if err := loadLocalEnv(); err != nil {
		t.Fatalf("loadLocalEnv() returned error: %v", err)
	}
	if got := os.Getenv("DBTOOLS_LOCAL_URL"); got != "mssql://sa:pw@localhost:14330?database=dbtools_local" {
		t.Fatalf("DBTOOLS_LOCAL_URL = %q, want %q", got, "mssql://sa:pw@localhost:14330?database=dbtools_local")
	}
}

func TestRunStartWritesLocalEnv(t *testing.T) {
	origLoadConfig := loadConfig
	origStartContainer := startContainer
	origWriteLocalEnv := writeLocalEnv
	t.Cleanup(func() {
		loadConfig = origLoadConfig
		startContainer = origStartContainer
		writeLocalEnv = origWriteLocalEnv
	})

	loadConfig = fakeLocalConfig("DBTOOLS_LOCAL_URL")
	startContainer = func(string, string, string, time.Duration, bool) (string, error) {
		return "mssql://sa:pw@localhost:14330?database=dbtools_local", nil
	}
	var wrote map[string]string
	writeLocalEnv = func(vars map[string]string) error {
		wrote = vars
		return nil
	}

	if err := runStart(); err != nil {
		t.Fatalf("runStart() returned error: %v", err)
	}
	if wrote["DBTOOLS_LOCAL_URL"] != "mssql://sa:pw@localhost:14330?database=dbtools_local" {
		t.Fatalf("runStart() wrote %#v", wrote)
	}
}

func TestRunStartUsesConfiguredURLEnv(t *testing.T) {
	origLoadConfig := loadConfig
	origStartContainer := startContainer
	origWriteLocalEnv := writeLocalEnv
	t.Cleanup(func() {
		loadConfig = origLoadConfig
		startContainer = origStartContainer
		writeLocalEnv = origWriteLocalEnv
	})

	loadConfig = fakeLocalConfig("CUSTOM_LOCAL_URL")
	startContainer = func(string, string, string, time.Duration, bool) (string, error) {
		return "mssql://sa:pw@localhost:14330?database=dbtools_local", nil
	}
	var wrote map[string]string
	writeLocalEnv = func(vars map[string]string) error {
		wrote = vars
		return nil
	}

	if err := runStart(); err != nil {
		t.Fatalf("runStart() returned error: %v", err)
	}
	if _, ok := wrote["CUSTOM_LOCAL_URL"]; !ok {
		t.Fatalf("runStart() wrote %#v, want key CUSTOM_LOCAL_URL from dbtools.toml", wrote)
	}
}

func TestRunStartMissingLocalTarget(t *testing.T) {
	origLoadConfig := loadConfig
	t.Cleanup(func() { loadConfig = origLoadConfig })

	loadConfig = func(string) (*config.Config, error) {
		return &config.Config{MigrationsDir: "migrations", Targets: map[string]config.Target{}}, nil
	}

	if err := runStart(); err == nil {
		t.Fatal("runStart() with no local target configured returned nil error, want error")
	}
}

func TestRunStopRemovesLocalEnv(t *testing.T) {
	origStopContainer := stopContainer
	origRemoveLocalEnv := removeLocalEnv
	t.Cleanup(func() {
		stopContainer = origStopContainer
		removeLocalEnv = origRemoveLocalEnv
	})

	stopContainer = func(string, string, bool) error { return nil }
	removed := false
	removeLocalEnv = func() error {
		removed = true
		return nil
	}

	if err := runStop(); err != nil {
		t.Fatalf("runStop() returned error: %v", err)
	}
	if !removed {
		t.Fatal("runStop() did not remove the local env file")
	}
}

func TestRunStartWritesFileRelativeToWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() returned error: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("os.Chdir() returned error: %v", err)
	}

	origLoadConfig := loadConfig
	origStartContainer := startContainer
	t.Cleanup(func() {
		loadConfig = origLoadConfig
		startContainer = origStartContainer
	})
	loadConfig = fakeLocalConfig("DBTOOLS_LOCAL_URL")
	startContainer = func(string, string, string, time.Duration, bool) (string, error) {
		return "mssql://sa:pw@localhost:14330?database=dbtools_local", nil
	}

	if err := runStart(); err != nil {
		t.Fatalf("runStart() returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(localenv.Dir, "local.env")); err != nil {
		t.Fatalf("local env file not created: %v", err)
	}
}
