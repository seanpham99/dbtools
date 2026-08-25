//go:build integration

package container

import (
	"testing"
	"time"
)

func TestMSSQLStartStopIdempotent(t *testing.T) {
	const projectID = "itest-mssql"

	url, err := StartForWithTimeout("mssql", projectID, "", 30*time.Second, true)
	if err != nil {
		t.Fatalf("StartForWithTimeout(mssql) returned error: %v", err)
	}
	if url == "" {
		t.Fatal("StartForWithTimeout(mssql) returned empty URL")
	}

	// Calling Start again while already running must be a no-op, not an error.
	url2, err := StartForWithTimeout("mssql", projectID, "", 30*time.Second, true)
	if err != nil {
		t.Fatalf("second StartForWithTimeout(mssql) returned error: %v", err)
	}
	if url2 != url {
		t.Errorf("second StartForWithTimeout(mssql) = %q, want %q", url2, url)
	}

	if err := StopFor("mssql", projectID, true); err != nil {
		t.Fatalf("StopFor(mssql, ..., purge=true) returned error: %v", err)
	}

	exists, _, err := inspect(containerNameFor("mssql", projectID))
	if err != nil {
		t.Fatalf("inspect() after StopFor returned error: %v", err)
	}
	if exists {
		t.Error("expected mssql container to be removed after StopFor")
	}
}
