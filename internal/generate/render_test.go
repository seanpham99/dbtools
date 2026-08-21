package generate

import (
	"strings"
	"testing"
)

func TestToPascalCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"fact_widget_sale", "FactWidgetSale"},
		{"dim_customer_account", "DimCustomerAccount"},
		{"api_request_log", "ApiRequestLog"}, // no acronym handling: title-cased like any other word
		{"trade-log", "TradeLog"},            // C6: non-identifier chars dropped
		{"2fa_codes", "_2faCodes"},           // C6: leading digit prefixed
		{"user id", "UserId"},                // C6: spaces split words
	}

	for _, tt := range tests {
		actual := ToPascalCase(tt.input)
		if actual != tt.expected {
			t.Errorf("ToPascalCase(%q) = %q; want %q", tt.input, actual, tt.expected)
		}
	}
}

func TestResolveClassNamesCollision(t *testing.T) {
	tables := []TableSchema{
		{Schema: "dbo", Name: "user"},
		{Schema: "audit", Name: "user"},
		{Schema: "sales", Name: "fact_price"},
	}

	resolved, err := ResolveClassNames(tables)
	if err != nil {
		t.Fatalf("ResolveClassNames returned error: %v", err)
	}
	if resolved[0].ClassName != "DboUser" {
		t.Errorf("expected DboUser, got %s", resolved[0].ClassName)
	}
	if resolved[1].ClassName != "AuditUser" {
		t.Errorf("expected AuditUser, got %s", resolved[1].ClassName)
	}
	if resolved[2].ClassName != "FactPrice" {
		t.Errorf("expected FactPrice, got %s", resolved[2].ClassName)
	}
}

func TestResolveClassNamesUnresolvableCollisionErrors(t *testing.T) {
	// dbo.user and audit.user disambiguate to DboUser/AuditUser, but a third
	// table literally named "dbo_user" also PascalCases to "DboUser".
	tables := []TableSchema{
		{Schema: "dbo", Name: "user"},
		{Schema: "audit", Name: "user"},
		{Schema: "other", Name: "dbo_user"},
	}

	if _, err := ResolveClassNames(tables); err == nil {
		t.Fatal("expected ResolveClassNames to error on an unresolvable class name collision, got nil")
	}
}

func TestMapMSSQLToPython(t *testing.T) {
	tests := []struct {
		input      string
		expected   string
		expectKnow bool
	}{
		{"bigint", "int", true},
		{"INT", "int", true},
		{"decimal", "Decimal", true},
		{"bit", "bool", true},
		{"nvarchar", "str", true},
		{"datetime2", "datetime", true},
		{"time", "time", true},
		{"uniqueidentifier", "UUID", true},
		{"varbinary", "bytes", true},
		{"unknown_type", "Any", false},
	}

	for _, tt := range tests {
		actual, known := MapMSSQLToPython(tt.input)
		if actual != tt.expected {
			t.Errorf("MapMSSQLToPython(%q) = %q; want %q", tt.input, actual, tt.expected)
		}
		if known != tt.expectKnow {
			t.Errorf("MapMSSQLToPython(%q) known = %v; want %v", tt.input, known, tt.expectKnow)
		}
	}
}

func TestSanitizeFieldName(t *testing.T) {
	if got := SanitizeFieldName("class"); got != "class_" {
		t.Errorf("SanitizeFieldName(class) = %q; want class_", got)
	}
	if got := SanitizeFieldName("ticker"); got != "ticker" {
		t.Errorf("SanitizeFieldName(ticker) = %q; want ticker (unchanged)", got)
	}
	if got := SanitizeFieldName("user id"); got != "userid" {
		t.Errorf("SanitizeFieldName(user id) = %q; want userid (C6: spaces dropped)", got)
	}
	if got := SanitizeFieldName("2fa"); got != "_2fa" {
		t.Errorf("SanitizeFieldName(2fa) = %q; want _2fa (C6: leading digit prefixed)", got)
	}
	if got := SanitizeFieldName("select"); got != "select" {
		t.Errorf("SanitizeFieldName(select) = %q; want select (not a keyword)", got)
	}
}

func TestRenderImportDeduplication(t *testing.T) {
	tables := []TableSchema{
		{
			Schema: "dbo",
			Name:   "simple",
			Columns: []ColumnSchema{
				{Name: "id", PyName: "id", PythonType: "int", IsNullable: false},
				{Name: "name", PyName: "name", PythonType: "str", IsNullable: true},
			},
		},
	}

	out, err := Render(tables, "local")
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	if strings.Contains(out, "from uuid import UUID") {
		t.Errorf("rendered output unexpectedly contains UUID import: %s", out)
	}
	if strings.Contains(out, "from decimal import Decimal") {
		t.Errorf("rendered output unexpectedly contains Decimal import: %s", out)
	}
	if !strings.Contains(out, "from typing import Optional") {
		t.Errorf("rendered output missing Optional import: %s", out)
	}
}

func TestRenderAllRequiredNoAnyProducesValidTypingImport(t *testing.T) {
	// Regression test: a schema with no nullable columns and no unmapped
	// types must not emit a bare "from typing import" line (SyntaxError).
	tables := []TableSchema{
		{
			Schema: "dbo",
			Name:   "simple",
			Columns: []ColumnSchema{
				{Name: "id", PyName: "id", PythonType: "int", IsNullable: false},
			},
		},
	}

	out, err := Render(tables, "local")
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if strings.Contains(out, "from typing import\n") || strings.Contains(out, "from typing import \n") {
		t.Errorf("rendered output has an empty typing import: %s", out)
	}
}

func TestRenderEmptyTableListProducesValidOutput(t *testing.T) {
	out, err := Render(nil, "local")
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if strings.Contains(out, "from typing import\n") || strings.Contains(out, "from typing import \n") {
		t.Errorf("rendered output has an empty typing import: %s", out)
	}
}

func TestRenderMatchesRuffFormatting(t *testing.T) {
	// Regression test: ruff (this repo's formatter) requires exactly two
	// blank lines between top-level classes and a blank line after a
	// class docstring. Assert both directly instead of shelling out to ruff.
	tables := []TableSchema{
		{
			Schema: "dbo",
			Name:   "first",
			Columns: []ColumnSchema{
				{Name: "id", PyName: "id", PythonType: "int"},
			},
		},
		{
			Schema: "dbo",
			Name:   "second",
			Columns: []ColumnSchema{
				{Name: "id", PyName: "id", PythonType: "int"},
			},
		},
	}

	out, err := Render(tables, "local")
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}

	if !strings.Contains(out, "    id: int = Field(alias=\"id\")\n\n\nclass Second(BaseModel):") {
		t.Errorf("expected exactly two blank lines between classes, got:\n%s", out)
	}
	if !strings.Contains(out, "class First(BaseModel):\n    \"\"\"dbo.first\"\"\"\n\n    id: int = Field(alias=\"id\")") {
		t.Errorf("expected a blank line after the class docstring, got:\n%s", out)
	}
}

func TestRenderFieldAliasRoundTripsOriginalName(t *testing.T) {
	// C6: a sanitized PyName must keep the original column name via
	// Field(alias=...), so the mapping still round-trips.
	tables := []TableSchema{
		{
			Schema: "dbo",
			Name:   "odd_table",
			Columns: []ColumnSchema{
				{Name: "2fa code", PyName: "_2facode", PythonType: "str", IsNullable: false},
			},
		},
	}

	out, err := Render(tables, "local")
	if err != nil {
		t.Fatalf("Render returned error: %v", err)
	}
	if !strings.Contains(out, `_2facode: str = Field(alias="2fa code")`) {
		t.Errorf("expected sanitized name + original alias, got:\n%s", out)
	}
}
