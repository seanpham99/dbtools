package sqliteengine

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/generate"
)

func TestIntrospect_FullStructuralCatalog(t *testing.T) {
	db, err := SQLite{}.Open("sqlite://" + filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`CREATE TABLE dbtools_it_customers (id INTEGER PRIMARY KEY, email TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE dbtools_it_text_keys (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE dbtools_it_implicit_fk (customer_id INTEGER, FOREIGN KEY (customer_id) REFERENCES dbtools_it_customers)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
CREATE TABLE dbtools_it_orders (
    id INTEGER PRIMARY KEY,
    customer_id INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    FOREIGN KEY (customer_id) REFERENCES dbtools_it_customers(id)
)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX idx_it_orders_status ON dbtools_it_orders (status)`); err != nil {
		t.Fatal(err)
	}

	tables, _, err := introspect(db, nil)
	if err != nil {
		t.Fatalf("introspect() returned error: %v", err)
	}

	var orders, customers, textKeys, implicitFK *generate.TableSchema
	for i := range tables {
		switch tables[i].Name {
		case "dbtools_it_orders":
			orders = &tables[i]
		case "dbtools_it_customers":
			customers = &tables[i]
		case "dbtools_it_text_keys":
			textKeys = &tables[i]
		case "dbtools_it_implicit_fk":
			implicitFK = &tables[i]
		}
	}
	if orders == nil {
		t.Fatal("dbtools_it_orders table not found")
	}

	for _, c := range orders.Columns {
		if c.Name == "id" && !c.IsPrimaryKey { //nolint:staticcheck // SA5011 false positive: t.Fatal above guarantees non-nil
			t.Error("id column: want IsPrimaryKey true")
		}
		if c.Name == "status" && (!c.DefaultValue.Valid || !strings.Contains(c.DefaultValue.String, "pending")) { //nolint:staticcheck // SA5011 false positive
			t.Errorf("status column DefaultValue = %+v, want it to mention 'pending'", c.DefaultValue)
		}
		if c.OrdinalPosition == 0 {
			t.Errorf("column %s: OrdinalPosition = 0, want a real 1-based position", c.Name)
		}
	}

	if len(orders.ForeignKeys) != 1 || orders.ForeignKeys[0].RefTable != "dbtools_it_customers" {
		t.Errorf("ForeignKeys = %+v, want one FK to dbtools_it_customers", orders.ForeignKeys)
	}
	if len(orders.Indexes) != 1 || orders.Indexes[0].Name != "idx_it_orders_status" || !orders.Indexes[0].Unique {
		t.Errorf("Indexes = %+v, want exactly idx_it_orders_status, unique", orders.Indexes)
	}
	if len(orders.CheckConstraints) != 0 {
		t.Errorf("CheckConstraints = %+v, want always empty for SQLite", orders.CheckConstraints)
	}
	if customers == nil || len(customers.Columns) == 0 || customers.Columns[0].IsNullable {
		t.Errorf("INTEGER PRIMARY KEY column = %+v, want non-nullable", customers)
	}
	if textKeys == nil || len(textKeys.Columns) == 0 || !textKeys.Columns[0].IsNullable {
		t.Errorf("TEXT PRIMARY KEY column = %+v, want nullable without NOT NULL", textKeys)
	}
	if implicitFK == nil || len(implicitFK.ForeignKeys) != 1 || len(implicitFK.ForeignKeys[0].RefColumns) != 1 || implicitFK.ForeignKeys[0].RefColumns[0] != "" {
		t.Errorf("implicit parent-key ForeignKeys = %+v, want one nullable referenced column represented as empty", implicitFK)
	}
}
