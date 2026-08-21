package generate

import (
	"strings"
	"testing"
)

func TestMapSQLToTS(t *testing.T) {
	cases := []struct {
		sql  string
		want string
	}{
		{"bigint", "bigint"},
		{"int8", "bigint"},
		{"bigserial", "bigint"},
		{"int", "number"},
		{"smallint", "number"},
		{"integer", "number"},
		{"serial", "number"},
		{"float", "number"},
		{"decimal", "number"},
		{"numeric", "number"},
		{"money", "number"},
		{"bit", "boolean"},
		{"boolean", "boolean"},
		{"varchar", "string"},
		{"text", "string"},
		{"uuid", "string"},
		{"uniqueidentifier", "string"},
		{"date", "string"},
		{"timestamp", "string"},
		{"timestamptz", "string"},
		{"json", "any"},
		{"jsonb", "any"},
		{"bytea", "any"},
		{"weird_unknown_type", "any"},
	}
	for _, c := range cases {
		if got := MapSQLToTS(c.sql); got != c.want {
			t.Errorf("MapSQLToTS(%q) = %q, want %q", c.sql, got, c.want)
		}
	}
}

func TestTSNameReservedWords(t *testing.T) {
	if got := TSName("class"); got != "class_" {
		t.Errorf("TSName(class) = %q, want class_", got)
	}
	if got := TSName("delete"); got != "delete_" {
		t.Errorf("TSName(delete) = %q, want delete_", got)
	}
	if got := TSName("email"); got != "email" {
		t.Errorf("TSName(email) = %q, want email", got)
	}
	if got := TSName("user_id"); got != "user_id" {
		t.Errorf("TSName(user_id) = %q, want user_id (snake_case preserved)", got)
	}
}

func TestRenderTSInterfaces(t *testing.T) {
	tables := []TableSchema{
		{
			Schema:    "dbo",
			Name:      "users",
			ClassName: "Users",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "int", IsNullable: false},
				{Name: "email", DataType: "varchar", IsNullable: true},
				{Name: "class", DataType: "varchar", IsNullable: false}, // reserved word
				{Name: "created_at", DataType: "timestamp", IsNullable: false},
			},
		},
	}

	out, err := RenderTS(tables, "local", false)
	if err != nil {
		t.Fatalf("RenderTS: %v", err)
	}

	for _, want := range []string{
		"export interface Users {",
		"  id: number;",
		"  email?: string;",
		"  class_: string;",
		"  created_at: string;",
		"}", // closing brace for the interface (non-greedy; we just check presence)
	} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderTS output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "z.object") {
		t.Errorf("RenderTS without --zod emitted zod schema:\n%s", out)
	}
}

func TestRenderTSWithZod(t *testing.T) {
	tables := []TableSchema{
		{
			Schema:    "public",
			Name:      "orders",
			ClassName: "Orders",
			Columns: []ColumnSchema{
				{Name: "id", DataType: "bigint", IsNullable: false},
				{Name: "note", DataType: "text", IsNullable: true},
			},
		},
	}

	out, err := RenderTS(tables, "local", true)
	if err != nil {
		t.Fatalf("RenderTS(withZod): %v", err)
	}

	for _, want := range []string{
		"export interface Orders {",
		"  id: bigint;",
		"  note?: string;",
		"import { z } from \"zod\";",
		"export const OrdersSchema = z.object({",
		"  id: z.bigint(),",
		"  note: z.string().nullish(),",
		"});",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderTS(withZod) missing %q:\n%s", want, out)
		}
	}
}
