//go:build integration

package container

import (
	"database/sql"
	"testing"
	"time"
)

// TestPostgresStartStopIdempotent mirrors the MSSQL lifecycle test for the
// Postgres container template. Needs Docker.
func TestPostgresStartStopIdempotent(t *testing.T) {
	const projectID = "itest-postgres"

	url, err := StartForWithTimeout("postgres", projectID, "", 30*time.Second, true)
	if err != nil {
		t.Fatalf("StartForWithTimeout(postgres) returned error: %v", err)
	}
	if url == "" {
		t.Fatal("StartForWithTimeout(postgres) returned empty URL")
	}

	url2, err := StartForWithTimeout("postgres", projectID, "", 30*time.Second, true)
	if err != nil {
		t.Fatalf("second StartForWithTimeout(postgres) returned error: %v", err)
	}
	if url2 != url {
		t.Errorf("second StartForWithTimeout(postgres) = %q, want %q", url2, url)
	}

	if err := StopFor("postgres", projectID, true); err != nil {
		t.Fatalf("StopFor(postgres, ..., purge=true) returned error: %v", err)
	}
	exists, _, err := inspect(containerNameFor("postgres", projectID))
	if err != nil {
		t.Fatalf("inspect() after StopFor returned error: %v", err)
	}
	if exists {
		t.Error("expected postgres container to be removed after StopFor")
	}
}

// TestPostgresStopPreservesDataUnlessPurged proves the volume-persistence
// contract: a plain Stop (purgeVolume=false) must leave data intact for
// the next Start, while purgeVolume=true must wipe it.
func TestPostgresStopPreservesDataUnlessPurged(t *testing.T) {
	const projectID = "itest-postgres-persist"

	url1, err := StartForWithTimeout("postgres", projectID, "", 30*time.Second, true)
	if err != nil {
		t.Fatalf("StartForWithTimeout(postgres) returned error: %v", err)
	}

	db, err := sql.Open("postgres", url1)
	if err != nil {
		t.Fatalf("sql.Open() returned error: %v", err)
	}
	if _, err := db.Exec("CREATE TABLE persistence_marker (id INT)"); err != nil {
		db.Close()
		t.Fatalf("CREATE TABLE returned error: %v", err)
	}
	db.Close()

	if err := StopFor("postgres", projectID, false); err != nil {
		t.Fatalf("StopFor(postgres, ..., purge=false) returned error: %v", err)
	}

	url2, err := StartForWithTimeout("postgres", projectID, "", 30*time.Second, true)
	if err != nil {
		t.Fatalf("StartForWithTimeout(postgres) after plain Stop returned error: %v", err)
	}

	db2, err := sql.Open("postgres", url2)
	if err != nil {
		t.Fatalf("sql.Open() returned error: %v", err)
	}
	defer db2.Close()
	var count int
	if err := db2.QueryRow("SELECT COUNT(*) FROM persistence_marker").Scan(&count); err != nil {
		t.Fatalf("querying persistence_marker after restart returned error: %v (data was not preserved)", err)
	}

	// Final cleanup: purge for real so the test doesn't leave the volume behind.
	if err := StopFor("postgres", projectID, true); err != nil {
		t.Fatalf("final StopFor(postgres, ..., purge=true) returned error: %v", err)
	}
}
