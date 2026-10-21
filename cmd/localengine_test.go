// cmd/localengine_test.go
package cmd

import (
	"errors"
	"testing"

	"github.com/seanpham99/dbtools/internal/config"
)

func TestLoadProjectID_DefaultsToPathHash(t *testing.T) {
	id, err := loadProjectID(&config.Config{})
	if err != nil {
		t.Fatalf("loadProjectID() returned error: %v", err)
	}
	if len(id) != 8 {
		t.Fatalf("loadProjectID() = %q, want an 8-char hash", id)
	}
}

func TestLoadProjectID_UsesConfiguredName(t *testing.T) {
	cfg := &config.Config{Project: config.ProjectConfig{Name: "myapp"}}
	id, err := loadProjectID(cfg)
	if err != nil {
		t.Fatalf("loadProjectID() returned error: %v", err)
	}
	if id != "myapp" {
		t.Fatalf("loadProjectID() = %q, want %q", id, "myapp")
	}
}

func TestLoadProjectID_RejectsInvalidName(t *testing.T) {
	cfg := &config.Config{Project: config.ProjectConfig{Name: "not valid!"}}
	if _, err := loadProjectID(cfg); err == nil {
		t.Fatal("loadProjectID() with an invalid [project] name returned nil error, want error")
	}
}

func TestLoadProjectIDOrDefault_FallsBackWithoutConfig(t *testing.T) {
	origLoadConfig := loadConfig
	t.Cleanup(func() { loadConfig = origLoadConfig })
	loadConfig = func(string) (*config.Config, error) {
		return nil, errors.New("dbtools.toml not found")
	}
	if id := loadProjectIDOrDefault(); len(id) != 8 {
		t.Fatalf("loadProjectIDOrDefault() = %q, want an 8-char hash even with no config", id)
	}
}

func TestConfiguredLocalPort_DefaultsToEmpty(t *testing.T) {
	if got := configuredLocalPort(&config.Config{}); got != "" {
		t.Fatalf("configuredLocalPort(zero-value) = %q, want empty (let the provider assign)", got)
	}
}

func TestConfiguredLocalPort_HonorsDeprecatedContainerPort(t *testing.T) {
	cfg := &config.Config{Container: config.ContainerConfig{Port: 55432}}
	if got := configuredLocalPort(cfg); got != "55432" {
		t.Fatalf("configuredLocalPort() = %q, want %q", got, "55432")
	}
}

func TestConfiguredLocalPort_LocalKeyWinsOverContainerKey(t *testing.T) {
	pinned := 55433
	cfg := &config.Config{
		Local:     config.LocalConfig{Port: &pinned},
		Container: config.ContainerConfig{Port: 55432},
	}
	if got := configuredLocalPort(cfg); got != "55433" {
		t.Fatalf("configuredLocalPort() = %q, want %q ([local] port must win)", got, "55433")
	}
}

func TestConfiguredLocalPort_ExplicitZeroUnpinsTheLegacyValue(t *testing.T) {
	zero := 0
	cfg := &config.Config{
		Local:     config.LocalConfig{Port: &zero},
		Container: config.ContainerConfig{Port: 55432},
	}
	if got := configuredLocalPort(cfg); got != "" {
		t.Fatalf("configuredLocalPort() = %q, want empty: an explicit [local] port = 0 must unpin, not fall back to the deprecated key", got)
	}
}
