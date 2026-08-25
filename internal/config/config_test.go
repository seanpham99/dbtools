package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "dbtools.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_DefaultsMigrationsDir(t *testing.T) {
	path := writeTemp(t, `
[targets.local]
url_env = "DBTOOLS_LOCAL_URL"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.MigrationsDir != "migrations" {
		t.Errorf("MigrationsDir = %q, want %q", cfg.MigrationsDir, "migrations")
	}
}

func TestLoad_ExplicitMigrationsDir(t *testing.T) {
	path := writeTemp(t, `
migrations_dir = "db/migrations"

[targets.local]
url_env = "DBTOOLS_LOCAL_URL"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.MigrationsDir != "db/migrations" {
		t.Errorf("MigrationsDir = %q, want %q", cfg.MigrationsDir, "db/migrations")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "does-not-exist.toml"))
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestResolveURL_Success(t *testing.T) {
	t.Setenv("DBTOOLS_LOCAL_URL", "mssql://sa:pw@localhost:1433?database=dbtools_test")
	cfg := &Config{Targets: map[string]Target{
		"local": {URLEnv: "DBTOOLS_LOCAL_URL"},
	}}
	url, err := cfg.ResolveURL("local")
	if err != nil {
		t.Fatalf("ResolveURL returned error: %v", err)
	}
	want := "mssql://sa:pw@localhost:1433?database=dbtools_test"
	if url != want {
		t.Errorf("ResolveURL = %q, want %q", url, want)
	}
}

func TestResolveURL_UnknownTarget(t *testing.T) {
	cfg := &Config{Targets: map[string]Target{}}
	_, err := cfg.ResolveURL("staging")
	if err == nil {
		t.Fatal("expected error for unknown target, got nil")
	}
}

func TestResolveURL_EnvVarNotSet(t *testing.T) {
	os.Unsetenv("DBTOOLS_MISSING_URL")
	cfg := &Config{Targets: map[string]Target{
		"staging": {URLEnv: "DBTOOLS_MISSING_URL"},
	}}
	_, err := cfg.ResolveURL("staging")
	if err == nil {
		t.Fatal("expected error for unset env var, got nil")
	}
}

func TestTargetNames_Sorted(t *testing.T) {
	cfg := &Config{Targets: map[string]Target{
		"staging": {URLEnv: "A"},
		"local":   {URLEnv: "B"},
		"prod":    {URLEnv: "C"},
	}}
	got := cfg.TargetNames()
	want := []string{"local", "prod", "staging"}
	if len(got) != len(want) {
		t.Fatalf("TargetNames() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("TargetNames() = %v, want %v", got, want)
		}
	}
}

func TestLoad_ParsesOptionalEngineField(t *testing.T) {
	path := writeTemp(t, `
[targets.local]
url_env = "L_URL"
engine = "mssql"

[targets.prod]
url_env = "P_URL"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if got := cfg.EngineName("local"); got != "mssql" {
		t.Errorf("EngineName(local) = %q, want mssql", got)
	}
	if got := cfg.EngineName("prod"); got != "" {
		t.Errorf("EngineName(prod) = %q, want \"\" (infer from URL scheme)", got)
	}
	if got := cfg.EngineName("nosuch"); got != "" {
		t.Errorf("EngineName(nosuch) = %q, want \"\"", got)
	}
}

func TestLoad_ParsesCloneConfig(t *testing.T) {
	path := writeTemp(t, `
[targets.local]
url_env = "L_URL"

[clone]
exclude = ["audit_log"]

[clone.mask]
email = "email"
phone = "redact"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if len(cfg.Clone.Exclude) != 1 || cfg.Clone.Exclude[0] != "audit_log" {
		t.Errorf("Clone.Exclude = %v, want [audit_log]", cfg.Clone.Exclude)
	}
	if cfg.Clone.Mask["email"] != "email" || cfg.Clone.Mask["phone"] != "redact" {
		t.Errorf("Clone.Mask = %v, want email->email, phone->redact", cfg.Clone.Mask)
	}
}

func TestLoad_CloneConfigDefaultsToEmpty(t *testing.T) {
	path := writeTemp(t, `
[targets.local]
url_env = "L_URL"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if len(cfg.Clone.Exclude) != 0 || len(cfg.Clone.Mask) != 0 {
		t.Errorf("Clone = %+v, want zero-value when [clone] is absent", cfg.Clone)
	}
}
