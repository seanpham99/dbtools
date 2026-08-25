package mysqlengine

import "testing"

func TestDSNFromURL_StripsSchemeAndForcesParseTime(t *testing.T) {
	got, err := dsnFromURL("mysql://root:secret@tcp(127.0.0.1:3306)/dbtools_local")
	if err != nil {
		t.Fatalf("dsnFromURL() returned error: %v", err)
	}
	want := "root:secret@tcp(127.0.0.1:3306)/dbtools_local?parseTime=true"
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
	want := "u:p@tcp(h:3306)/d?parseTime=true&tls=skip-verify"
	if got != want {
		t.Errorf("dsnFromURL() = %q, want %q", got, want)
	}
}

func TestDSNFromURL_InvalidDSN(t *testing.T) {
	if _, err := dsnFromURL("mysql://not a valid dsn!!!"); err == nil {
		t.Fatal("dsnFromURL() with a malformed DSN returned nil error, want an error")
	}
}
