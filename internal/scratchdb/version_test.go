package scratchdb

import "testing"

// MySQL's series is major.minor because 8.0 and 8.4 render expression
// defaults and CHECK clauses differently and ship as separate images.
func TestMajorMinor(t *testing.T) {
	cases := map[string]string{
		"8.0.36":         "8.0",
		"8.4.2":          "8.4",
		"8.4":            "8.4",
		"5.7.44-log":     "5.7",
		"8":              "",
		"":               "",
		".5":             "",
		"8.0.36-0ubuntu": "8.0",
	}
	for in, want := range cases {
		if got := majorMinor(in); got != want {
			t.Errorf("majorMinor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestItoa(t *testing.T) {
	cases := map[int]string{16: "16", 8: "8", 2019: "2019", 0: "", -1: ""}
	for in, want := range cases {
		if got := itoa(in); got != want {
			t.Errorf("itoa(%d) = %q, want %q", in, got, want)
		}
	}
}
