package scratchdb_test

import (
	"os"
	"testing"

	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/engine/sqliteengine"
	"github.com/seanpham99/dbtools/internal/scratchdb"
)

func TestProvision_AgainstSkipsProvisioning(t *testing.T) {
	eng, err := engine.ForName("sqlite")
	if err != nil {
		t.Fatal(err)
	}
	url, cleanup, err := scratchdb.Provision(eng, "sqlite:///already/there.db")
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if url != "sqlite:///already/there.db" {
		t.Errorf("url = %q, want the against value unchanged", url)
	}
	if cleanup != nil {
		t.Error("cleanup should be nil when against skips provisioning — nothing to tear down")
	}
}

func TestProvision_SQLiteCreatesAndCleansUpTempfile(t *testing.T) {
	eng, err := engine.ForName("sqlite")
	if err != nil {
		t.Fatal(err)
	}
	url, cleanup, err := scratchdb.Provision(eng, "")
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	path := sqliteengine.PathFromURL(url)
	db, err := eng.Open(url)
	if err != nil {
		t.Fatalf("eng.Open(%q): %v", url, err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("db.Ping(): %v", err)
	}
	db.Close()
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("expected scratch file to exist at %s: %v", path, statErr)
	}
	if cleanup == nil {
		t.Fatal("cleanup should not be nil for a provisioned sqlite scratch file")
	}
	if err := cleanup(); err != nil {
		t.Errorf("cleanup() returned error: %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("scratch file still exists after cleanup: statErr = %v", statErr)
	}
}
