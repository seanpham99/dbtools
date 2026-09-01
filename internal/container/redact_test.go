package container

import (
	"strings"
	"testing"
)

// A malformed local URL must not echo its password back into the error,
// which main prints to stderr (and CI logs collect).
func TestMaintenanceURLFor_RedactsPasswordInError(t *testing.T) {
	_, _, err := mysqlURLPort("mysql://root:supersecret@localhost/db")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "supersecret") {
		t.Fatalf("error leaks password: %q", err.Error())
	}
	if !strings.Contains(err.Error(), ":***@") {
		t.Fatalf("error should contain redacted URL: %q", err.Error())
	}
}

func TestMaintenanceURLFor_NonLoopbackRedactsPassword(t *testing.T) {
	_, err := MaintenanceURLFor("mysql", "mysql://root:supersecret@db.example.com:3306/dbtools_local")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "supersecret") {
		t.Fatalf("error leaks password: %q", err.Error())
	}
}
