package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/statusinfo"
)

func TestUpJSON_SingleDocument(t *testing.T) {
	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll("migrations", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("migrations", "20260817000001_users.up.sql"), []byte(`CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL);`), 0o644); err != nil {
		t.Fatal(err)
	}

	dbURL := "sqlite://" + filepath.Join(dir, "local.db")
	t.Setenv("DBTOOLS_LOCAL_URL", dbURL)
	cfg := &config.Config{
		MigrationsDir: "migrations",
		Targets:       map[string]config.Target{"local": {URLEnv: "DBTOOLS_LOCAL_URL"}},
	}
	origLoadConfig := loadConfig
	loadConfig = func(string) (*config.Config, error) { return cfg, nil }
	t.Cleanup(func() { loadConfig = origLoadConfig })

	origJSONOutput := jsonOutput
	origTarget := upTarget
	jsonOutput = true
	upTarget = "local"
	t.Cleanup(func() {
		jsonOutput = origJSONOutput
		upTarget = origTarget
	})

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	rootCmd.SetArgs([]string{"up", "--json"})
	err = rootCmd.Execute()

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("up --json returned error: %v", err)
	}

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	out := strings.TrimSpace(buf.String())

	lines := strings.Split(out, "\n")
	if len(lines) != 1 {
		t.Fatalf("up --json stdout has %d lines (%q), want exactly 1 JSON document", len(lines), out)
	}

	var parsed struct {
		statusinfo.Status
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &parsed); err != nil {
		t.Fatalf("failed to unmarshal JSON document %q: %v", lines[0], err)
	}
	if !parsed.OK {
		t.Errorf("parsed ok = false, want true")
	}
	if parsed.CurrentVersion != 20260817000001 {
		t.Errorf("parsed current_version = %d, want 20260817000001", parsed.CurrentVersion)
	}
}

func TestUpSilenceUsageOnRefusal(t *testing.T) {
	origSilenceUsage := upCmd.SilenceUsage
	upCmd.SilenceUsage = false
	t.Cleanup(func() { upCmd.SilenceUsage = origSilenceUsage })

	var out strings.Builder
	rootCmd.SetErr(&out)
	rootCmd.SetOut(&out)
	t.Cleanup(func() { rootCmd.SetErr(nil); rootCmd.SetOut(nil) })

	rootCmd.SetArgs([]string{"up", "--target", "prod"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("up --target prod expected error, got nil")
	}
	if strings.Contains(out.String(), "Usage:") {
		t.Fatalf("up operational refusal printed usage block:\n%s", out.String())
	}
}
