package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/logger"
)

func TestLogFormatIntegration_JSON(t *testing.T) {
	t.Cleanup(resetRootFlags)
	resetRootFlags()

	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
		upTarget = "local"
		upURL = ""
		upDryRun = false
		pushYes = false
		pushURL = ""
		pushDryRun = false
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll("migrations", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("migrations", "20260825000001_test.up.sql"), []byte("CREATE TABLE log_test (id INT);"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("migrations", "20260825000001_test.down.sql"), []byte("DROP TABLE log_test;"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbURL := "sqlite://" + filepath.Join(dir, "log_test.db")
	t.Setenv("DBTOOLS_TEST_LOG_URL", dbURL)

	configContent := `migrations_dir = "migrations"

[targets.local]
url_env = "DBTOOLS_TEST_LOG_URL"

[targets.remote]
url_env = "DBTOOLS_TEST_LOG_URL"
`
	if err := os.WriteFile("dbtools.toml", []byte(configContent), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	logger.SetOutput(&buf)

	rootCmd.SetArgs([]string{"--log-format=json", "up"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("rootCmd.Execute() up failed: %v", err)
	}

	logOutput := strings.TrimSpace(buf.String())
	if logOutput == "" {
		t.Fatal("expected log output, got empty")
	}

	lines := strings.Split(logOutput, "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var record logger.LogRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("failed to unmarshal log record as JSON: %v, raw line: %q", err, line)
		}
		if record.Timestamp == "" {
			t.Errorf("expected non-empty timestamp, got %q in %s", record.Timestamp, line)
		}
		if record.Level != "INFO" {
			t.Errorf("expected level INFO, got %q in %s", record.Level, line)
		}
		if !strings.Contains(record.Message, "local: now at version") {
			t.Errorf("expected message containing 'local: now at version', got %q", record.Message)
		}
	}
}

func TestLogFormatIntegration_Text(t *testing.T) {
	t.Cleanup(resetRootFlags)
	resetRootFlags()

	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
		upTarget = "local"
		upURL = ""
		upDryRun = false
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll("migrations", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("migrations", "20260825000001_test.up.sql"), []byte("CREATE TABLE log_test_text (id INT);"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbURL := "sqlite://" + filepath.Join(dir, "log_test_text.db")
	t.Setenv("DBTOOLS_TEST_LOG_TEXT_URL", dbURL)

	configContent := `migrations_dir = "migrations"

[targets.local]
url_env = "DBTOOLS_TEST_LOG_TEXT_URL"
`
	if err := os.WriteFile("dbtools.toml", []byte(configContent), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	logger.SetOutput(&buf)

	rootCmd.SetArgs([]string{"--log-format=text", "up"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("rootCmd.Execute() up failed: %v", err)
	}

	logOutput := strings.TrimSpace(buf.String())
	if logOutput == "" {
		t.Fatal("expected log output, got empty")
	}

	if strings.HasPrefix(logOutput, "{") {
		t.Fatalf("expected plain text log, but got JSON-like output: %q", logOutput)
	}
	if !strings.Contains(logOutput, "local: now at version") {
		t.Fatalf("expected message containing 'local: now at version', got %q", logOutput)
	}
}

func TestLogFormatIntegration_PushJSON(t *testing.T) {
	t.Cleanup(resetRootFlags)
	resetRootFlags()

	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
		pushYes = false
		pushURL = ""
		pushDryRun = false
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll("migrations", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("migrations", "20260825000001_test.up.sql"), []byte("CREATE TABLE log_test_push (id INT);"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbURL := "sqlite://" + filepath.Join(dir, "log_test_push.db")
	t.Setenv("DBTOOLS_TEST_PUSH_LOG_URL", dbURL)

	configContent := `migrations_dir = "migrations"

[targets.remote]
url_env = "DBTOOLS_TEST_PUSH_LOG_URL"
`
	if err := os.WriteFile("dbtools.toml", []byte(configContent), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	logger.SetOutput(&buf)

	rootCmd.SetArgs([]string{"--log-format=json", "push", "remote", "--yes"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("rootCmd.Execute() push failed: %v", err)
	}

	logOutput := strings.TrimSpace(buf.String())
	if logOutput == "" {
		t.Fatal("expected log output, got empty")
	}

	var record logger.LogRecord
	if err := json.Unmarshal([]byte(logOutput), &record); err != nil {
		t.Fatalf("failed to unmarshal log record as JSON: %v, raw line: %q", err, logOutput)
	}
	if record.Timestamp == "" || record.Level != "INFO" || !strings.Contains(record.Message, "remote: now at version") {
		t.Fatalf("unexpected record: %+v", record)
	}
}
