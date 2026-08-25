// internal/container/container_test.go
package container

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestLocalDatabaseURLFor(t *testing.T) {
	got, err := LocalDatabaseURLFor("mssql", "14330")
	if err != nil {
		t.Fatalf("LocalDatabaseURLFor(mssql, 14330) returned error: %v", err)
	}
	want := "mssql://sa:" + url.QueryEscape(password) + "@127.0.0.1:14330?database=" + DatabaseName + "&TrustServerCertificate=true"
	if got != want {
		t.Fatalf("LocalDatabaseURLFor(mssql, 14330) = %q, want %q", got, want)
	}
}

func TestLocalDatabaseURLFor_UnknownEngine(t *testing.T) {
	if _, err := LocalDatabaseURLFor("oracle", "1"); err == nil {
		t.Fatal("LocalDatabaseURLFor(oracle, ...) returned nil error, want error")
	}
}

func TestMaintenanceURLFor_Postgres(t *testing.T) {
	local := "postgres://postgres:pw@127.0.0.1:54329/dbtools_local?sslmode=disable"
	got, err := MaintenanceURLFor("postgres", local)
	if err != nil {
		t.Fatalf("MaintenanceURLFor(postgres, ...) returned error: %v", err)
	}
	want := "postgres://postgres:" + url.QueryEscape(password) + "@127.0.0.1:54329/postgres?sslmode=disable"
	if got != want {
		t.Fatalf("MaintenanceURLFor(postgres, ...) = %q, want %q", got, want)
	}
}

func TestMaintenanceURLFor_MSSQL(t *testing.T) {
	local := "mssql://sa:pw@127.0.0.1:14331?database=dbtools_local&TrustServerCertificate=true"
	got, err := MaintenanceURLFor("mssql", local)
	if err != nil {
		t.Fatalf("MaintenanceURLFor(mssql, ...) returned error: %v", err)
	}
	want := "mssql://sa:" + url.QueryEscape(password) + "@127.0.0.1:14331?database=master&TrustServerCertificate=true"
	if got != want {
		t.Fatalf("MaintenanceURLFor(mssql, ...) = %q, want %q", got, want)
	}
}

func TestMaintenanceURLFor_MySQL(t *testing.T) {
	local := "mysql://root:pw@tcp(127.0.0.1:33061)/dbtools_local"
	got, err := MaintenanceURLFor("mysql", local)
	if err != nil {
		t.Fatalf("MaintenanceURLFor(mysql, ...) returned error: %v", err)
	}
	want := "mysql://root:" + url.QueryEscape(password) + "@tcp(127.0.0.1:33061)/"
	if got != want {
		t.Fatalf("MaintenanceURLFor(mysql, ...) = %q, want %q", got, want)
	}
}

func TestMaintenanceURLFor_MySQLMissingTCPSyntax(t *testing.T) {
	if _, err := MaintenanceURLFor("mysql", "mysql://root:pw@127.0.0.1:3306/db"); err == nil {
		t.Fatal("MaintenanceURLFor(mysql, ...) with no tcp(host:port) syntax returned nil error, want error")
	}
}

func TestContainerNameFor(t *testing.T) {
	got := containerNameFor("postgres", "abcd1234")
	want := "dbtools-postgres-abcd1234"
	if got != want {
		t.Fatalf("containerNameFor(postgres, abcd1234) = %q, want %q", got, want)
	}
}

func TestVolumeNameFor(t *testing.T) {
	got := volumeNameFor("dbtools-postgres-abcd1234")
	want := "dbtools-postgres-abcd1234-data"
	if got != want {
		t.Fatalf("volumeNameFor(dbtools-postgres-abcd1234) = %q, want %q", got, want)
	}
}

func TestSpecFor_UnknownEngineListsSupported(t *testing.T) {
	_, err := specFor("oracle")
	if err == nil {
		t.Fatal("specFor(oracle) returned nil error, want error")
	}
	if got := err.Error(); !(strings.Contains(got, "mssql") && strings.Contains(got, "postgres") && strings.Contains(got, "mysql")) {
		t.Fatalf("specFor(oracle) error = %q, want it to list mssql, postgres, and mysql", got)
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
