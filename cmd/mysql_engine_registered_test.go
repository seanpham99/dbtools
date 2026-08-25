package cmd

import (
	"testing"

	"github.com/seanpham99/dbtools/internal/engine"
	_ "github.com/seanpham99/dbtools/internal/engine/mssqlengine"
	_ "github.com/seanpham99/dbtools/internal/engine/postgresengine"
	_ "github.com/seanpham99/dbtools/internal/engine/sqliteengine"
)

// This package (cmd) is what actually wires every engine into the dbtools
// binary via its blank imports in root.go. This test asserts mysql is
// among them without depending on root.go's import list directly (so it
// still fails clearly if the import is missing or removed later).
func TestMySQLEngineRegisteredInBinary(t *testing.T) {
	names := engine.Names()
	for _, n := range names {
		if n == "mysql" {
			return
		}
	}
	t.Fatalf("engine.Names() = %v, want it to include \"mysql\" (is internal/engine/mysqlengine imported from cmd/root.go?)", names)
}
