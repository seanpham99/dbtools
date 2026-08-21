package mssqlengine

import "testing"

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
