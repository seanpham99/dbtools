package mysqlengine

import (
	"database/sql"
	"testing"

	"github.com/seanpham99/dbtools/internal/generate"
	_ "modernc.org/sqlite"
)

func TestMapMySQLToPython(t *testing.T) {
	tests := []struct {
		input      string
		expected   string
		expectKnow bool
	}{
		{"int", "int", true},
		{"BIGINT", "int", true},
		{"tinyint", "int", true},
		{"decimal", "Decimal", true},
		{"double", "float", true},
		{"varchar", "str", true},
		{"enum", "str", true},
		{"datetime", "datetime", true},
		{"timestamp", "datetime", true},
		{"time", "time", true},
		{"blob", "bytes", true},
		{"json", "Any", true},
		{"geometry", "Any", false},
	}
	for _, tt := range tests {
		actual, known := MapMySQLToPython(tt.input)
		if actual != tt.expected {
			t.Errorf("MapMySQLToPython(%q) = %q; want %q", tt.input, actual, tt.expected)
		}
		if known != tt.expectKnow {
			t.Errorf("MapMySQLToPython(%q) known = %v; want %v", tt.input, known, tt.expectKnow)
		}
	}
}

func TestIntrospectCheckConstraints_Unsupported(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`ATTACH DATABASE ':memory:' AS information_schema`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE information_schema.tables (table_schema TEXT, table_name TEXT)`); err != nil {
		t.Fatal(err)
	}

	original := &generate.TableSchema{Name: "orders", Columns: []generate.ColumnSchema{{Name: "id"}}}
	tableMap := map[string]*generate.TableSchema{"orders": original}
	if err := introspectCheckConstraints(db, tableMap); err != nil {
		t.Fatalf("introspectCheckConstraints() returned error for unsupported metadata: %v", err)
	}
	if tableMap["orders"] != original || len(original.Columns) != 1 || len(original.CheckConstraints) != 0 {
		t.Errorf("tableMap = %+v, want previously collected schema data preserved", tableMap)
	}
}
