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

	if _, err := db.Exec(`CREATE TABLE customers (id INTEGER PRIMARY KEY, email TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
CREATE TABLE orders (
    id INTEGER PRIMARY KEY,
    customer_id INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    FOREIGN KEY (customer_id) REFERENCES customers(id)
)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX idx_orders_status ON orders (status)`); err != nil {
		t.Fatal(err)
	}

	tables, _, err := introspect(db, nil)
	if err != nil {
		t.Fatalf("introspect() returned error: %v", err)
	}

	var orders *generate.TableSchema
	for i := range tables {
		if tables[i].Name == "orders" {
			orders = &tables[i]
		}
	}
	if orders == nil {
		t.Fatal("orders table not found")
	}

	for _, c := range orders.Columns {
		if c.Name == "id" && !c.IsPrimaryKey {
			t.Error("id column: want IsPrimaryKey true")
		}
		if c.Name == "status" && (!c.DefaultValue.Valid || !strings.Contains(c.DefaultValue.String, "pending")) {
			t.Errorf("status column DefaultValue = %+v, want it to mention 'pending'", c.DefaultValue)
		}
		if c.OrdinalPosition == 0 {
			t.Errorf("column %s: OrdinalPosition = 0, want a real 1-based position", c.Name)
		}
	}

	if len(orders.ForeignKeys) != 1 || orders.ForeignKeys[0].RefTable != "customers" {
		t.Errorf("ForeignKeys = %+v, want one FK to customers", orders.ForeignKeys)
	}
	if len(orders.Indexes) != 1 || orders.Indexes[0].Name != "idx_orders_status" || !orders.Indexes[0].Unique {
		t.Errorf("Indexes = %+v, want exactly idx_orders_status, unique", orders.Indexes)
	}
	if len(orders.CheckConstraints) != 0 {
		t.Errorf("CheckConstraints = %+v, want always empty for SQLite", orders.CheckConstraints)
	}
}
