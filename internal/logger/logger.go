package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Level represents the severity level of a log message.
type Level string

const (
	LevelDebug Level = "DEBUG"
	LevelInfo  Level = "INFO"
	LevelWarn  Level = "WARN"
	LevelError Level = "ERROR"
)

// String returns the string representation of the level.
func (l Level) String() string {
	return string(l)
}

// Supported log format types.
const (
	FormatText = "text"
	FormatJSON = "json"
)

// LogRecord represents a single structured log entry for JSON output.
type LogRecord struct {
	Timestamp string         `json:"timestamp"`
	Level     string         `json:"level"`
	Message   string         `json:"message"`
	Fields    map[string]any `json:"fields,omitempty"`
}

// Logger provides synchronized structured logging to an io.Writer.
type Logger struct {
	mu     sync.Mutex
	out    io.Writer
	format string
}

// New creates a new Logger writing to w with default text format.
func New(w io.Writer) *Logger {
	if w == nil {
		w = os.Stderr
	}
	return &Logger{
		out:    w,
		format: FormatText,
	}
}

var defaultLogger = func() *Logger {
	l := New(os.Stderr)
	if env := os.Getenv("DBTOOLS_LOG_FORMAT"); env != "" {
		l.SetFormat(env)
	}
	return l
}()

// Default returns the package-level default logger instance.
func Default() *Logger {
	return defaultLogger
}

// SetFormat configures the log format ("text" or "json").
func (l *Logger) SetFormat(format string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	switch strings.ToLower(strings.TrimSpace(format)) {
	case FormatJSON:
		l.format = FormatJSON
	default:
		l.format = FormatText
	}
}

// GetFormat returns the current log format.
func (l *Logger) GetFormat() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.format
}

// SetOutput sets the output writer for the logger.
func (l *Logger) SetOutput(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if w == nil {
		w = os.Stderr
	}
	l.out = w
}

func (l *Logger) writeRecord(level Level, msg string, fields map[string]any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.format == FormatJSON {
		rec := LogRecord{
			Timestamp: time.Now().UTC().Format(time.RFC3339),
			Level:     string(level),
			Message:   msg,
			Fields:    fields,
		}
		data, err := json.Marshal(rec)
		if err != nil {
			fallback := fmt.Sprintf(`{"timestamp":%q,"level":"ERROR","message":%q}`+"\n",
				time.Now().UTC().Format(time.RFC3339), "failed to marshal log record: "+err.Error())
			_, _ = io.WriteString(l.out, fallback)
			return
		}
		data = append(data, '\n')
		_, _ = l.out.Write(data)
		return
	}

	_, _ = fmt.Fprintln(l.out, msg)
}

// Log writes a message with a custom level and optional fields.
func (l *Logger) Log(level Level, msg string, fields map[string]any) {
	l.writeRecord(level, msg, fields)
}

// Debug logs a message at LevelDebug.
func (l *Logger) Debug(msg string) {
	l.writeRecord(LevelDebug, msg, nil)
}

// Debugf logs a formatted message at LevelDebug.
func (l *Logger) Debugf(format string, args ...any) {
	l.writeRecord(LevelDebug, fmt.Sprintf(format, args...), nil)
}

// Info logs a message at LevelInfo.
func (l *Logger) Info(msg string) {
	l.writeRecord(LevelInfo, msg, nil)
}

// Infof logs a formatted message at LevelInfo.
func (l *Logger) Infof(format string, args ...any) {
	l.writeRecord(LevelInfo, fmt.Sprintf(format, args...), nil)
}

// Warn logs a message at LevelWarn.
func (l *Logger) Warn(msg string) {
	l.writeRecord(LevelWarn, msg, nil)
}

// Warnf logs a formatted message at LevelWarn.
func (l *Logger) Warnf(format string, args ...any) {
	l.writeRecord(LevelWarn, fmt.Sprintf(format, args...), nil)
}

// Error logs a message at LevelError.
func (l *Logger) Error(msg string) {
	l.writeRecord(LevelError, msg, nil)
}

// Errorf logs a formatted message at LevelError.
func (l *Logger) Errorf(format string, args ...any) {
	l.writeRecord(LevelError, fmt.Sprintf(format, args...), nil)
}

// Package-level functions delegating to defaultLogger:

// SetFormat configures the default logger's format ("text" or "json").
func SetFormat(format string) {
	defaultLogger.SetFormat(format)
}

// GetFormat returns the default logger's format.
func GetFormat() string {
	return defaultLogger.GetFormat()
}

// SetOutput sets the default logger's output writer.
func SetOutput(w io.Writer) {
	defaultLogger.SetOutput(w)
}

// Info logs a message at LevelInfo using the default logger.
func Info(msg string) {
	defaultLogger.Info(msg)
}

// Infof logs a formatted message at LevelInfo using the default logger.
func Infof(format string, args ...any) {
	defaultLogger.Infof(format, args...)
}

// Warn logs a message at LevelWarn using the default logger.
func Warn(msg string) {
	defaultLogger.Warn(msg)
}

// Warnf logs a formatted message at LevelWarn using the default logger.
func Warnf(format string, args ...any) {
	defaultLogger.Warnf(format, args...)
}

// Error logs a message at LevelError using the default logger.
func Error(msg string) {
	defaultLogger.Error(msg)
}

// Errorf logs a formatted message at LevelError using the default logger.
func Errorf(format string, args ...any) {
	defaultLogger.Errorf(format, args...)
}

// Debug logs a message at LevelDebug using the default logger.
func Debug(msg string) {
	defaultLogger.Debug(msg)
}

// Debugf logs a formatted message at LevelDebug using the default logger.
func Debugf(format string, args ...any) {
	defaultLogger.Debugf(format, args...)
}

// Log writes a message with a custom level and optional fields using the default logger.
func Log(level Level, msg string, fields map[string]any) {
	defaultLogger.Log(level, msg, fields)
}
