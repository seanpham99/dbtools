package engine_test

import (
	"path/filepath"
	"testing"

	"github.com/seanpham99/dbtools/internal/engine"
	_ "github.com/seanpham99/dbtools/internal/engine/sqliteengine"
)

func TestTableExists(t *testing.T) {
	eng, err := engine.ForName("sqlite")
	if err != nil {
		t.Fatal(err)
	}
	db, err := eng.Open("sqlite://" + filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	exists, err := engine.TableExists(eng, db, "not_there")
	if err != nil {
		t.Fatalf("TableExists() returned error: %v", err)
	}
	if exists {
		t.Error("TableExists() = true for a table that was never created")
	}

	if _, err := db.Exec(`CREATE TABLE present (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	exists, err = engine.TableExists(eng, db, "present")
	if err != nil {
		t.Fatalf("TableExists() returned error: %v", err)
	}
	if !exists {
		t.Error("TableExists() = false for a table that was just created")
	}
}
