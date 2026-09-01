package mssqlengine

import (
	"strings"
	"testing"
)

// url.Error renders the raw input, so a parse failure must not carry the
// password into the wrapped error main prints to stderr.
func TestRewriteToSQLServerScheme_RedactsPasswordInParseError(t *testing.T) {
	_, err := RewriteToSQLServerScheme("mssql://sa:supersecret@host:9x9/db")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "supersecret") {
		t.Fatalf("error leaks password: %q", err.Error())
	}
}
