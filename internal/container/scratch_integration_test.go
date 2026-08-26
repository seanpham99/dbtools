//go:build integration

package container

import (
	"database/sql"
	"testing"
)

func TestStartScratch_PostgresIntegration(t *testing.T) {
	url, cleanup, err := StartScratch("postgres")
	if err != nil {
		t.Fatalf("StartScratch(postgres) returned error: %v", err)
	}
	if url == "" {
		t.Fatal("StartScratch(postgres) returned empty URL")
	}
	if cleanup == nil {
		t.Fatal("StartScratch(postgres) returned nil cleanup function")
	}

	db, err := sql.Open("postgres", url)
	if err != nil {
		cleanup()
		t.Fatalf("sql.Open returned error: %v", err)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		cleanup()
		t.Fatalf("db.Ping returned error: %v", err)
	}
	db.Close()

	if err := cleanup(); err != nil {
		t.Fatalf("cleanup() returned error: %v", err)
	}
}
