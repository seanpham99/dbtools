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

func TestConfiguredContainerPort_DefaultsToEmpty(t *testing.T) {
	if got := configuredContainerPort(&config.Config{}); got != "" {
		t.Fatalf("configuredContainerPort(zero-value) = %q, want empty (let Docker assign)", got)
	}
}

func TestConfiguredContainerPort_UsesConfiguredValue(t *testing.T) {
	cfg := &config.Config{Container: config.ContainerConfig{Port: 55432}}
	if got := configuredContainerPort(cfg); got != "55432" {
		t.Fatalf("configuredContainerPort() = %q, want %q", got, "55432")
	}
}
