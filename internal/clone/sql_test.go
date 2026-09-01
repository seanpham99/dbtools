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
	got := buildSelectSQL("postgres", "public", "customers", 0, "")
	want := `SELECT * FROM "public"."customers"`
	if got != want {
		t.Errorf("buildSelectSQL() = %q, want %q", got, want)
	}
}

func TestBuildSelectSQL_LimitOnNonMSSQL(t *testing.T) {
	got := buildSelectSQL("sqlite", "main", "customers", 10, "")
	want := `SELECT * FROM "main"."customers" LIMIT 10`
	if got != want {
		t.Errorf("buildSelectSQL() = %q, want %q", got, want)
	}
}

func TestBuildSelectSQL_LimitOnMSSQLUsesTOP(t *testing.T) {
	got := buildSelectSQL("mssql", "dbo", "customers", 10, "")
	want := "SELECT TOP 10 * FROM [dbo].[customers]"
	if got != want {
		t.Errorf("buildSelectSQL() = %q, want %q", got, want)
	}
}

func TestBuildSelectSQL_Where(t *testing.T) {
	got := buildSelectSQL("postgres", "public", "orders", 0, "status = 'Shipped'")
	want := `SELECT * FROM "public"."orders" WHERE status = 'Shipped'`
	if got != want {
		t.Errorf("buildSelectSQL() = %q, want %q", got, want)
	}
}

func TestBuildSelectSQL_WhereAndLimitMSSQL(t *testing.T) {
	got := buildSelectSQL("mssql", "dbo", "orders", 5, "status = 'Shipped'")
	want := "SELECT TOP 5 * FROM [dbo].[orders] WHERE status = 'Shipped'"
	if got != want {
		t.Errorf("buildSelectSQL() = %q, want %q", got, want)
	}
}

func TestBuildInsertSQL(t *testing.T) {
	got := buildInsertSQL("postgres", "public", "customers", []string{"id", "name", "email"})
	want := `INSERT INTO "public"."customers" ("id", "name", "email") VALUES ($1, $2, $3)`
	if got != want {
		t.Errorf("buildInsertSQL() = %q, want %q", got, want)
	}
}

func TestBuildInsertSQL_MSSQLPlaceholders(t *testing.T) {
	got := buildInsertSQL("mssql", "dbo", "customers", []string{"id", "name"})
	want := "INSERT INTO [dbo].[customers] ([id], [name]) VALUES (@p1, @p2)"
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

func TestQuoteQualified_UsesSchema(t *testing.T) {
	cases := []struct {
		engineName, schema, name, want string
	}{
		{"postgres", "billing", "orders", `"billing"."orders"`},
		{"mssql", "sales", "orders", "[sales].[orders]"},
		{"mysql", "shop", "orders", "`shop`.`orders`"},
		{"postgres", "", "orders", `"orders"`},
	}
	for _, c := range cases {
		if got := quoteQualified(c.engineName, c.schema, c.name); got != c.want {
			t.Errorf("quoteQualified(%q, %q, %q) = %q, want %q", c.engineName, c.schema, c.name, got, c.want)
		}
	}
}
