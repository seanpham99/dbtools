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
	sc, err := scratchdb.Provision(eng, "sqlite:///already/there.db")
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if sc.URL != "sqlite:///already/there.db" {
		t.Errorf("url = %q, want the against value unchanged", sc.URL)
	}
	if sc.Cleanup != nil {
		t.Error("sc.Cleanup should be nil when against skips provisioning — nothing to tear down")
	}
}

func TestProvision_SQLiteCreatesAndCleansUpTempfile(t *testing.T) {
	eng, err := engine.ForName("sqlite")
	if err != nil {
		t.Fatal(err)
	}
	sc, err := scratchdb.Provision(eng, "")
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	path := sqliteengine.PathFromURL(sc.URL)
	db, err := eng.Open(sc.URL)
	if err != nil {
		t.Fatalf("eng.Open(%q): %v", sc.URL, err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("db.Ping(): %v", err)
	}
	db.Close()
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("expected scratch file to exist at %s: %v", path, statErr)
	}
	if sc.Cleanup == nil {
		t.Fatal("sc.Cleanup should not be nil for a provisioned sqlite scratch file")
	}
	if err := sc.Cleanup(); err != nil {
		t.Errorf("sc.Cleanup() returned error: %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("scratch file still exists after sc.Cleanup: statErr = %v", statErr)
	}
}
