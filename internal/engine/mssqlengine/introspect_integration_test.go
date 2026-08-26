//go:build integration

package mssqlengine

import (
	"os"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/generate"
	"github.com/seanpham99/dbtools/internal/testdb"
)

func TestIntrospect_FullStructuralCatalog(t *testing.T) {
	url := os.Getenv("DBTOOLS_TEST_MSSQL_URL")
	if url == "" {
		t.Skip("DBTOOLS_TEST_MSSQL_URL not set, skipping integration test")
	}
	if err := testdb.ResetTracking(url); err != nil {
		t.Fatal(err)
	}
	db, err := Open(url)
	if err != nil {
		t.Fatalf("Open() returned error: %v", err)
	}
	defer db.Close()

	cleanup := func() {
		db.Exec(`DROP TABLE IF EXISTS dbtools_it_orders`)
		db.Exec(`DROP TABLE IF EXISTS dbtools_it_customers`)
	}
	cleanup()
	t.Cleanup(cleanup)

	if _, err := db.Exec(`
CREATE TABLE dbtools_it_customers (
    id INT NOT NULL,
    email NVARCHAR(255) NOT NULL,
    PRIMARY KEY (id)
)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
CREATE TABLE dbtools_it_orders (
    id INT NOT NULL,
    customer_id INT NOT NULL,
    status NVARCHAR(50) NOT NULL DEFAULT 'pending',
    total DECIMAL(10,2) NOT NULL,
    PRIMARY KEY (id),
    CONSTRAINT fk_it_customer FOREIGN KEY (customer_id) REFERENCES dbtools_it_customers(id),
    CONSTRAINT chk_it_total CHECK (total >= 0)
)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE UNIQUE INDEX idx_it_orders_status ON dbtools_it_orders (status) INCLUDE (total)`); err != nil {
		t.Fatal(err)
	}

	tables, _, err := Introspect(db, nil)
	if err != nil {
		t.Fatalf("Introspect() returned error: %v", err)
	}

	var orders *generate.TableSchema
	for i := range tables {
		if tables[i].Name == "dbtools_it_orders" {
			orders = &tables[i]
		}
	}
	if orders == nil {
		t.Fatal("dbtools_it_orders table not found")
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

	if len(orders.ForeignKeys) != 1 || orders.ForeignKeys[0].RefTable != "dbtools_it_customers" {
		t.Errorf("ForeignKeys = %+v, want one FK to dbtools_it_customers", orders.ForeignKeys)
	}
	if len(orders.CheckConstraints) != 1 || !strings.Contains(orders.CheckConstraints[0].Expression, "total") {
		t.Errorf("CheckConstraints = %+v, want one mentioning total", orders.CheckConstraints)
	}
	if len(orders.Indexes) != 1 || orders.Indexes[0].Name != "idx_it_orders_status" || !orders.Indexes[0].Unique ||
		strings.Join(orders.Indexes[0].Columns, ",") != "status" {
		t.Errorf("Indexes = %+v, want exactly idx_it_orders_status, unique on status with INCLUDE columns excluded", orders.Indexes)
	}
}
