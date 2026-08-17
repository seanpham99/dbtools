package container

import (
	"errors"
	"net/url"
	"testing"
)

func TestLocalDatabaseURL(t *testing.T) {
	want := "mssql://sa:" + url.QueryEscape(password) + "@127.0.0.1:" + Port + "?database=" + DatabaseName + "&TrustServerCertificate=true"
	if got := LocalDatabaseURL(); got != want {
		t.Fatalf("LocalDatabaseURL() = %q, want %q", got, want)
	}
}

func TestMasterURL(t *testing.T) {
	want := "mssql://sa:" + url.QueryEscape(password) + "@127.0.0.1:" + Port + "?database=master&TrustServerCertificate=true"
	if got := MasterURL(); got != want {
		t.Fatalf("MasterURL() = %q, want %q", got, want)
	}
}

func TestParseInspectOutputNoSuchContainer(t *testing.T) {
	exists, running, err := parseInspectOutput([]byte("Error: No such container: dbtools-mssql-local\n"), errors.New("exit status 1"))
	if err != nil {
		t.Fatalf("parseInspectOutput() returned error: %v", err)
	}
	if exists || running {
		t.Fatalf("parseInspectOutput() = exists=%v running=%v, want false false", exists, running)
	}
}

func TestParseInspectOutputNoSuchObject(t *testing.T) {
	// Docker CLI 25+ wording: "error: no such object: <name>" instead of
	// the older "Error: No such container: <name>".
	exists, running, err := parseInspectOutput([]byte("error: no such object: dbtools-mssql-local\n"), errors.New("exit status 1"))
	if err != nil {
		t.Fatalf("parseInspectOutput() returned error: %v", err)
	}
	if exists || running {
		t.Fatalf("parseInspectOutput() = exists=%v running=%v, want false false", exists, running)
	}
}

func TestParseInspectOutputRunning(t *testing.T) {
	exists, running, err := parseInspectOutput([]byte("true\n"), nil)
	if err != nil {
		t.Fatalf("parseInspectOutput() returned error: %v", err)
	}
	if !exists || !running {
		t.Fatalf("parseInspectOutput() = exists=%v running=%v, want true true", exists, running)
	}
}

func TestParseInspectOutputStopped(t *testing.T) {
	exists, running, err := parseInspectOutput([]byte("false\n"), nil)
	if err != nil {
		t.Fatalf("parseInspectOutput() returned error: %v", err)
	}
	if !exists || running {
		t.Fatalf("parseInspectOutput() = exists=%v running=%v, want true false", exists, running)
	}
}

func TestParseInspectOutputOtherError(t *testing.T) {
	_, _, err := parseInspectOutput([]byte("Cannot connect to the Docker daemon"), errors.New("exit status 1"))
	if err == nil {
		t.Fatal("parseInspectOutput() returned nil error, want non-nil")
	}
}
