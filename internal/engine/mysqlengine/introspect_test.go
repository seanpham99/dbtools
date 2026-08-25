package mysqlengine

import "testing"

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
