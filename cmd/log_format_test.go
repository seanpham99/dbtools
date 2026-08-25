package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/logger"
)

func resetRootFlags() {
	jsonOutput = false
	logFormat = "text"
	if f := rootCmd.PersistentFlags().Lookup("log-format"); f != nil {
		f.Changed = false
		_ = f.Value.Set("text")
	}
	if f := rootCmd.PersistentFlags().Lookup("json"); f != nil {
		f.Changed = false
		_ = f.Value.Set("false")
	}
	logger.SetFormat("text")
	logger.SetOutput(os.Stderr)
}

func TestLogFormat_JSONFlag(t *testing.T) {
	t.Cleanup(resetRootFlags)
	resetRootFlags()

	rootCmd.SetArgs([]string{"--log-format=json", "version"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("expected rootCmd.Execute() to succeed, got: %v", err)
	}

	if got := logger.GetFormat(); got != "json" {
		t.Fatalf("logger.GetFormat() = %q, want %q", got, "json")
	}
}

func TestLogFormat_TextFlag(t *testing.T) {
	t.Cleanup(resetRootFlags)
	resetRootFlags()

	rootCmd.SetArgs([]string{"--log-format=text", "version"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("expected rootCmd.Execute() to succeed, got: %v", err)
	}

	if got := logger.GetFormat(); got != "text" {
		t.Fatalf("logger.GetFormat() = %q, want %q", got, "text")
	}
}

func TestLogFormat_InvalidFlag(t *testing.T) {
	t.Cleanup(resetRootFlags)
	resetRootFlags()

	rootCmd.SetArgs([]string{"--log-format=invalid", "version"})
	err := rootCmd.Execute()
	if err == nil {
		t.Fatal("expected validation error for invalid --log-format, got nil")
	}

	expected := `invalid --log-format "invalid" (must be 'text' or 'json')`
	if !strings.Contains(err.Error(), expected) {
		t.Fatalf("expected error containing %q, got %q", expected, err.Error())
	}
}

func TestLogFormat_EnvVarFallback(t *testing.T) {
	t.Cleanup(resetRootFlags)
	resetRootFlags()

	t.Setenv("DBTOOLS_LOG_FORMAT", "json")

	rootCmd.SetArgs([]string{"version"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("expected rootCmd.Execute() to succeed, got: %v", err)
	}

	if got := logger.GetFormat(); got != "json" {
		t.Fatalf("logger.GetFormat() = %q, want %q", got, "json")
	}
}

func TestLogFormat_FlagOverridesEnvVar(t *testing.T) {
	t.Cleanup(resetRootFlags)
	resetRootFlags()

	t.Setenv("DBTOOLS_LOG_FORMAT", "json")

	rootCmd.SetArgs([]string{"--log-format=text", "version"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("expected rootCmd.Execute() to succeed, got: %v", err)
	}

	if got := logger.GetFormat(); got != "text" {
		t.Fatalf("logger.GetFormat() = %q, want %q", got, "text")
	}
}

func TestLogFormat_JSONOutputStderr(t *testing.T) {
	t.Cleanup(resetRootFlags)
	resetRootFlags()

	var buf bytes.Buffer
	logger.SetOutput(&buf)

	rootCmd.SetArgs([]string{"--json", "version"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("expected rootCmd.Execute() to succeed, got: %v", err)
	}

	// Logging after pre-run with --json must not write to custom buf because output was reset to os.Stderr
	logger.Info("should go to stderr")
	if buf.Len() != 0 {
		t.Fatalf("expected buf to be empty after --json redirected output to os.Stderr, got %q", buf.String())
	}
}
