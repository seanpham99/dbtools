package mysqlengine

import (
	"strings"
	"testing"
)

func TestDSNFromURL_StripsSchemeAndForcesParseTime(t *testing.T) {
	got, err := dsnFromURL("mysql://root:secret@tcp(127.0.0.1:3306)/dbtools_local")
	if err != nil {
		t.Fatalf("dsnFromURL() returned error: %v", err)
	}
	want := "root:secret@tcp(127.0.0.1:3306)/dbtools_local?multiStatements=true&parseTime=true"
	if got != want {
		t.Errorf("dsnFromURL() = %q, want %q", got, want)
	}
}

func TestDSNFromURL_PreservesExistingParamsAndOverridesParseTime(t *testing.T) {
	// A caller-supplied parseTime=false must not silently defeat the
	// DATETIME-scanning requirement — dbtools always forces it on.
	got, err := dsnFromURL("mysql://u:p@tcp(h:3306)/d?parseTime=false&tls=skip-verify")
	if err != nil {
		t.Fatalf("dsnFromURL() returned error: %v", err)
	}
	want := "u:p@tcp(h:3306)/d?multiStatements=true&parseTime=true&tls=skip-verify"
	if got != want {
		t.Errorf("dsnFromURL() = %q, want %q", got, want)
	}
}

func TestDSNFromURL_InvalidDSN(t *testing.T) {
	if _, err := dsnFromURL("mysql://not a valid dsn!!!"); err == nil {
		t.Fatal("dsnFromURL() with a malformed DSN returned nil error, want an error")
	}
}

// MultiStatements must be forced, not left to the caller. Without it MySQL
// executes only the first statement of a multi-statement migration file and
// reports success, so the ledger records a version that was never fully
// applied — silent schema drift, which is the failure dbtools exists to
// prevent.
func TestDSNFromURL_ForcesMultiStatements(t *testing.T) {
	dsn, err := dsnFromURL("mysql://u:p@tcp(h:3306)/d")
	if err != nil {
		t.Fatalf("dsnFromURL() returned error: %v", err)
	}
	if !strings.Contains(dsn, "multiStatements=true") {
		t.Errorf("dsnFromURL() = %q, want it to force multiStatements=true", dsn)
	}
}
