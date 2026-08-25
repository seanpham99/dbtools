package logger_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/seanpham99/dbtools/internal/logger"
)

func TestTextFormat(t *testing.T) {
	var buf bytes.Buffer
	l := logger.New(&buf)
	l.SetFormat("text")

	if got := l.GetFormat(); got != logger.FormatText {
		t.Fatalf("expected format %q, got %q", logger.FormatText, got)
	}

	l.Info("info message")
	l.Infof("info formatted %d", 42)
	l.Warn("warn message")
	l.Warnf("warn formatted %s", "foo")
	l.Error("error message")
	l.Errorf("error formatted %s", "bar")
	l.Debug("debug message")
	l.Debugf("debug formatted %s", "baz")
	l.Log(logger.LevelInfo, "custom level message", map[string]any{"key": "value"})

	expected := "info message\n" +
		"info formatted 42\n" +
		"warn message\n" +
		"warn formatted foo\n" +
		"error message\n" +
		"error formatted bar\n" +
		"debug message\n" +
		"debug formatted baz\n" +
		"custom level message\n"

	if buf.String() != expected {
		t.Errorf("unexpected output:\ngot:\n%s\nwant:\n%s", buf.String(), expected)
	}
}

func TestJSONFormat(t *testing.T) {
	var buf bytes.Buffer
	l := logger.New(&buf)
	l.SetFormat("json")

	if got := l.GetFormat(); got != logger.FormatJSON {
		t.Fatalf("expected format %q, got %q", logger.FormatJSON, got)
	}

	before := time.Now().UTC().Add(-time.Second)

	l.Info("starting process")
	l.Infof("item count: %d", 10)
	l.Warn("resource warning")
	l.Warnf("threshold reached: %v", true)
	l.Error("operation failed")
	l.Errorf("code: %d", 500)
	l.Debug("verbose details")
	l.Debugf("step: %s", "init")
	l.Log(logger.LevelWarn, "with fields", map[string]any{
		"target":  "postgres",
		"version": 16,
	})

	after := time.Now().UTC().Add(time.Second)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 9 {
		t.Fatalf("expected 9 lines, got %d:\n%s", len(lines), buf.String())
	}

	expectedLevels := []string{
		"INFO", "INFO", "WARN", "WARN", "ERROR", "ERROR", "DEBUG", "DEBUG", "WARN",
	}
	expectedMessages := []string{
		"starting process",
		"item count: 10",
		"resource warning",
		"threshold reached: true",
		"operation failed",
		"code: 500",
		"verbose details",
		"step: init",
		"with fields",
	}

	for i, line := range lines {
		var rec logger.LogRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line %d is not valid JSON: %v (raw: %s)", i, err, line)
		}

		if rec.Level != expectedLevels[i] {
			t.Errorf("line %d: expected level %s, got %s", i, expectedLevels[i], rec.Level)
		}
		if rec.Message != expectedMessages[i] {
			t.Errorf("line %d: expected message %q, got %q", i, expectedMessages[i], rec.Message)
		}

		ts, err := time.Parse(time.RFC3339, rec.Timestamp)
		if err != nil {
			t.Errorf("line %d: invalid RFC3339 timestamp %q: %v", i, rec.Timestamp, err)
		}
		if ts.Before(before) || ts.After(after) {
			t.Errorf("line %d: timestamp %v out of range [%v, %v]", i, ts, before, after)
		}

		if i == 8 {
			if rec.Fields == nil {
				t.Fatalf("line 8: expected fields, got nil")
			}
			if rec.Fields["target"] != "postgres" {
				t.Errorf("line 8: expected target=postgres, got %v", rec.Fields["target"])
			}
			if rec.Fields["version"] != float64(16) { // json unmarshals numbers to float64
				t.Errorf("line 8: expected version=16, got %v", rec.Fields["version"])
			}
		} else {
			if len(rec.Fields) != 0 {
				t.Errorf("line %d: expected empty fields, got %v", i, rec.Fields)
			}
		}
	}
}

func TestSetFormatValidationFallback(t *testing.T) {
	l := logger.New(nil)

	invalidFormats := []string{"", "xml", "yaml", "JSON_PRETTY", "123"}
	for _, fmtStr := range invalidFormats {
		l.SetFormat(fmtStr)
		if got := l.GetFormat(); got != logger.FormatText {
			t.Errorf("SetFormat(%q): expected fallback to %q, got %q", fmtStr, logger.FormatText, got)
		}
	}

	validJSONFormats := []string{"json", "JSON", " Json ", "  json  "}
	for _, fmtStr := range validJSONFormats {
		l.SetFormat(fmtStr)
		if got := l.GetFormat(); got != logger.FormatJSON {
			t.Errorf("SetFormat(%q): expected %q, got %q", fmtStr, logger.FormatJSON, got)
		}
	}
}

func TestPackageLevelDelegation(t *testing.T) {
	var buf bytes.Buffer
	logger.SetOutput(&buf)
	logger.SetFormat("text")
	defer func() {
		logger.SetOutput(nil)
		logger.SetFormat("text")
	}()

	logger.Info("pkg info")
	logger.Infof("pkg infof %d", 1)
	logger.Warn("pkg warn")
	logger.Warnf("pkg warnf %d", 2)
	logger.Error("pkg error")
	logger.Errorf("pkg errorf %d", 3)
	logger.Debug("pkg debug")
	logger.Debugf("pkg debugf %d", 4)
	logger.Log(logger.LevelInfo, "pkg log", map[string]any{"k": "v"})

	expected := "pkg info\n" +
		"pkg infof 1\n" +
		"pkg warn\n" +
		"pkg warnf 2\n" +
		"pkg error\n" +
		"pkg errorf 3\n" +
		"pkg debug\n" +
		"pkg debugf 4\n" +
		"pkg log\n"

	if buf.String() != expected {
		t.Errorf("unexpected output from package-level functions:\ngot:\n%s\nwant:\n%s", buf.String(), expected)
	}

	buf.Reset()
	logger.SetFormat("json")
	if got := logger.GetFormat(); got != logger.FormatJSON {
		t.Fatalf("expected format %q, got %q", logger.FormatJSON, got)
	}

	logger.Info("pkg json info")
	var rec logger.LogRecord
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("failed to parse json log: %v", err)
	}
	if rec.Level != "INFO" || rec.Message != "pkg json info" {
		t.Errorf("unexpected record: %+v", rec)
	}
}

func TestConcurrentLogging(t *testing.T) {
	var buf bytes.Buffer
	l := logger.New(&buf)
	l.SetFormat("json")

	const goroutines = 20
	const messagesPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < messagesPerGoroutine; j++ {
				l.Log(logger.LevelInfo, fmt.Sprintf("worker-%d message-%d", id, j), map[string]any{
					"worker": id,
					"iter":   j,
				})
			}
		}(i)
	}

	wg.Wait()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	expectedCount := goroutines * messagesPerGoroutine
	if len(lines) != expectedCount {
		t.Fatalf("expected %d log lines, got %d", expectedCount, len(lines))
	}

	for i, line := range lines {
		var rec logger.LogRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("line %d invalid JSON under concurrency: %v\nline: %s", i, err, line)
		}
	}
}

func TestLevelString(t *testing.T) {
	if logger.LevelDebug.String() != "DEBUG" {
		t.Errorf("expected DEBUG, got %s", logger.LevelDebug.String())
	}
	if logger.LevelInfo.String() != "INFO" {
		t.Errorf("expected INFO, got %s", logger.LevelInfo.String())
	}
	if logger.LevelWarn.String() != "WARN" {
		t.Errorf("expected WARN, got %s", logger.LevelWarn.String())
	}
	if logger.LevelError.String() != "ERROR" {
		t.Errorf("expected ERROR, got %s", logger.LevelError.String())
	}
}
