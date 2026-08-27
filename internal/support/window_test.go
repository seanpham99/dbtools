package support

import (
	"strings"
	"testing"
)

func TestCheck_InsideWindow(t *testing.T) {
	inside := [][2]string{
		{"postgres", "15"}, {"postgres", "16"}, {"postgres", "17"}, {"postgres", "18"},
		{"mysql", "8.0"}, {"mysql", "8.4"},
		{"mssql", "2019"}, {"mssql", "2022"},
	}
	for _, c := range inside {
		if ok, msg := Check(c[0], c[1]); !ok {
			t.Errorf("Check(%q, %q) = false (%s), want inside the window", c[0], c[1], msg)
		}
	}
}

func TestCheck_OutsideWindow(t *testing.T) {
	outside := [][2]string{
		{"postgres", "13"}, {"postgres", "14"}, {"postgres", "19"},
		{"mysql", "5.7"}, {"mysql", "9.0"},
		{"mssql", "2016"}, {"mssql", "2025"},
	}
	for _, c := range outside {
		ok, msg := Check(c[0], c[1])
		if ok {
			t.Errorf("Check(%q, %q) = true, want outside the window", c[0], c[1])
			continue
		}
		// The message has to name the version and the range, or it is not
		// actionable.
		if !strings.Contains(msg, c[1]) {
			t.Errorf("Check(%q, %q) message %q does not name the version", c[0], c[1], msg)
		}
		if !strings.Contains(msg, Windows[c[0]].Display) {
			t.Errorf("Check(%q, %q) message %q does not name the tested range", c[0], c[1], msg)
		}
	}
}

// Warn, never refuse: an unknown engine or an undetectable version must not
// stop a read-only command against someone's old production server.
func TestCheck_SaysNothingWhenItCannotTell(t *testing.T) {
	quiet := [][2]string{
		{"sqlite", "3"},
		{"postgres", ""},
		{"unknown-engine", "42"},
		{"postgres", "not-a-version"},
	}
	for _, c := range quiet {
		if ok, msg := Check(c[0], c[1]); !ok || msg != "" {
			t.Errorf("Check(%q, %q) = (%v, %q), want a silent pass", c[0], c[1], ok, msg)
		}
	}
}

func TestParse(t *testing.T) {
	cases := map[string]int{"15": 1500, "18": 1800, "8.0": 800, "8.4": 804, "2019": 201900}
	for in, want := range cases {
		got, ok := parse(in)
		if !ok || got != want {
			t.Errorf("parse(%q) = (%d, %v), want (%d, true)", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", ".", "8.", ".8", "8.0.1", "abc", "15a"} {
		if _, ok := parse(bad); ok {
			t.Errorf("parse(%q) succeeded, want failure", bad)
		}
	}
}
