package clone

import "testing"

func TestPlaceholder(t *testing.T) {
	cases := []struct {
		engineName string
		i          int
		want       string
	}{
		{"mssql", 1, "@p1"},
		{"mssql", 3, "@p3"},
		{"postgres", 1, "$1"},
		{"postgres", 2, "$2"},
		{"sqlite", 1, "?"},
		{"mysql", 1, "?"},
	}
	for _, c := range cases {
		if got := placeholder(c.engineName, c.i); got != c.want {
			t.Errorf("placeholder(%q, %d) = %q, want %q", c.engineName, c.i, got, c.want)
		}
	}
}

func TestBuildSelectSQL_NoLimitNoWhere(t *testing.T) {
	got := buildSelectSQL("postgres", "customers", 0, "")
	want := `SELECT * FROM "customers"`
	if got != want {
		t.Errorf("buildSelectSQL() = %q, want %q", got, want)
	}
}

func TestBuildSelectSQL_LimitOnNonMSSQL(t *testing.T) {
	got := buildSelectSQL("sqlite", "customers", 10, "")
	want := `SELECT * FROM "customers" LIMIT 10`
	if got != want {
		t.Errorf("buildSelectSQL() = %q, want %q", got, want)
	}
}

func TestBuildSelectSQL_LimitOnMSSQLUsesTOP(t *testing.T) {
	got := buildSelectSQL("mssql", "customers", 10, "")
	want := "SELECT TOP 10 * FROM [customers]"
	if got != want {
		t.Errorf("buildSelectSQL() = %q, want %q", got, want)
	}
}

func TestBuildSelectSQL_Where(t *testing.T) {
	got := buildSelectSQL("postgres", "orders", 0, "status = 'Shipped'")
	want := `SELECT * FROM "orders" WHERE status = 'Shipped'`
	if got != want {
		t.Errorf("buildSelectSQL() = %q, want %q", got, want)
	}
}

func TestBuildSelectSQL_WhereAndLimitMSSQL(t *testing.T) {
	got := buildSelectSQL("mssql", "orders", 5, "status = 'Shipped'")
	want := "SELECT TOP 5 * FROM [orders] WHERE status = 'Shipped'"
	if got != want {
		t.Errorf("buildSelectSQL() = %q, want %q", got, want)
	}
}

func TestBuildInsertSQL(t *testing.T) {
	got := buildInsertSQL("postgres", "customers", []string{"id", "name", "email"})
	want := `INSERT INTO "customers" ("id", "name", "email") VALUES ($1, $2, $3)`
	if got != want {
		t.Errorf("buildInsertSQL() = %q, want %q", got, want)
	}
}

func TestBuildInsertSQL_MSSQLPlaceholders(t *testing.T) {
	got := buildInsertSQL("mssql", "customers", []string{"id", "name"})
	want := "INSERT INTO [customers] ([id], [name]) VALUES (@p1, @p2)"
	if got != want {
		t.Errorf("buildInsertSQL() = %q, want %q", got, want)
	}
}

func TestQuoteIdentifierEscapesHostileNames(t *testing.T) {
	cases := []struct {
		engineName, name, want string
	}{
		{"postgres", `we"ird`, `"we""ird"`},
		{"sqlite", `we"ird`, `"we""ird"`},
		{"mssql", "we]ird", "[we]]ird]"},
		{"mysql", "we`ird", "`we``ird`"},
	}
	for _, c := range cases {
		if got := quoteIdentifier(c.engineName, c.name); got != c.want {
			t.Errorf("quoteIdentifier(%q, %q) = %q, want %q", c.engineName, c.name, got, c.want)
		}
	}
}
