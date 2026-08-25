//go:build integration

package container

import (
	"testing"
	"time"
)

// TestMySQLStartStopIdempotent mirrors the Postgres/MSSQL lifecycle
// tests for the MySQL container template. Needs Docker. MySQL's image
// takes longer to become ready than Postgres/MSSQL, hence the longer
// timeout.
func TestMySQLStartStopIdempotent(t *testing.T) {
	const projectID = "itest-mysql"

	url, err := StartForWithTimeout("mysql", projectID, "", 60*time.Second, true)
	if err != nil {
		t.Fatalf("StartForWithTimeout(mysql) returned error: %v", err)
	}
	if url == "" {
		t.Fatal("StartForWithTimeout(mysql) returned empty URL")
	}

	url2, err := StartForWithTimeout("mysql", projectID, "", 60*time.Second, true)
	if err != nil {
		t.Fatalf("second StartForWithTimeout(mysql) returned error: %v", err)
	}
	if url2 != url {
		t.Errorf("second StartForWithTimeout(mysql) = %q, want %q", url2, url)
	}

	if err := StopFor("mysql", projectID, true); err != nil {
		t.Fatalf("StopFor(mysql, ..., purge=true) returned error: %v", err)
	}
	exists, _, err := inspect(containerNameFor("mysql", projectID))
	if err != nil {
		t.Fatalf("inspect() after StopFor returned error: %v", err)
	}
	if exists {
		t.Error("expected mysql container to be removed after StopFor")
	}
}
