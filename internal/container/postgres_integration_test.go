//go:build integration

package container

import "testing"

// TestPostgresStartStopIdempotent mirrors the MSSQL lifecycle test for the
// Postgres container template. Needs Docker.
func TestPostgresStartStopIdempotent(t *testing.T) {
	url, err := StartFor("postgres")
	if err != nil {
		t.Fatalf("StartFor(postgres) returned error: %v", err)
	}
	want, err := LocalDatabaseURLFor("postgres")
	if err != nil {
		t.Fatalf("LocalDatabaseURLFor(postgres) returned error: %v", err)
	}
	if url != want {
		t.Errorf("StartFor(postgres) = %q, want %q", url, want)
	}

	url2, err := StartFor("postgres")
	if err != nil {
		t.Fatalf("second StartFor(postgres) returned error: %v", err)
	}
	if url2 != url {
		t.Errorf("second StartFor(postgres) = %q, want %q", url2, url)
	}

	if err := StopFor("postgres"); err != nil {
		t.Fatalf("StopFor(postgres) returned error: %v", err)
	}
	exists, _, err := inspect(postgresSpec.name)
	if err != nil {
		t.Fatalf("inspect() after StopFor returned error: %v", err)
	}
	if exists {
		t.Error("expected postgres container to be removed after StopFor")
	}
}
