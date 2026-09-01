package redact

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"postgres with password", "postgres://user:secret@localhost:5432/db", "postgres://user:***@localhost:5432/db"},
		{"mssql with password", "mssql://sa:P%40ss@localhost:1433?database=x", "mssql://sa:***@localhost:1433?database=x"},
		{"mysql tcp DSN", "root:pw@tcp(localhost:3306)/db", "root:***@tcp(localhost:3306)/db"},
		{"no password", "postgres://user@localhost:5432/db", "postgres://user@localhost:5432/db"},
		{"no userinfo", "postgres://localhost:5432/db", "postgres://localhost:5432/db"},
		{"port not mistaken for password", "postgres://localhost:5432/db", "postgres://localhost:5432/db"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := URL(tt.in); got != tt.want {
				t.Errorf("URL(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestURLStripsPasswordWhenSecretInInput(t *testing.T) {
	out := URL("garbage that is not a url user:secret@ embedded")
	if strings.Contains(out, "secret") {
		t.Errorf("URL should strip password from unparseable input: %q", out)
	}
}

func TestParseErrorStripsRawURL(t *testing.T) {
	// Assembled at runtime: a literal "mssql://...:9x9/..." would trip
	// staticcheck SA1007 (the string really is an invalid URL — on purpose).
	rawURL := fmt.Sprintf("mssql://user:%s@host:9x9/db", "secret")
	_, err := url.Parse(rawURL)
	if err == nil {
		t.Fatal("expected parse error")
	}
	if !errors.As(err, new(*url.Error)) {
		t.Fatal("expected *url.Error")
	}
	wrapped := fmt.Errorf("parsing mssql:// URL: %w", err)
	reason := ParseError(wrapped).Error()
	if strings.Contains(reason, "secret") || strings.Contains(reason, "mssql://") {
		t.Errorf("ParseError still leaks the raw URL: %q", reason)
	}
}
