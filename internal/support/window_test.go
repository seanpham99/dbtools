package support

import (
	"strings"
	"testing"
)

// The values here are what scratchdb.ServerSeries actually produces, which
// is the whole point: SQL Server reports ProductMajorVersion (16), never
// the year. An earlier version of this window compared against 2019/2022
// and therefore flagged *every* supported SQL Server target as untested —
// a bug the tests missed because they fed in the years too.
func TestCheck_InsideWindow(t *testing.T) {
	inside := [][2]string{
		{"postgres", "15"}, {"postgres", "16"}, {"postgres", "17"}, {"postgres", "18"},
		{"mysql", "8.0"}, {"mysql", "8.4"},
		{"mssql", "15"}, {"mssql", "16"},
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
		// The innovation releases between the LTS lines are not claimed.
		{"mysql", "8.1"}, {"mysql", "8.2"}, {"mysql", "8.3"},
		{"mssql", "13"}, {"mssql", "17"},
	}
	for _, c := range outside {
		ok, msg := Check(c[0], c[1])
		if ok {
			t.Errorf("Check(%q, %q) = true, want outside the window", c[0], c[1])
			continue
		}
		if !strings.Contains(msg, Windows[c[0]].Display) {
			t.Errorf("Check(%q, %q) message %q does not name the tested range", c[0], c[1], msg)
		}
	}
}

// A SQL Server warning must quote the year, not the major, or it names a
// version the operator has never seen on a download page.
func TestCheck_MSSQLWarningUsesTheReleaseYear(t *testing.T) {
	_, msg := Check("mssql", "17")
	if !strings.Contains(msg, "2025") {
		t.Errorf("message = %q, want it to name SQL Server 2025", msg)
	}
	if strings.Contains(msg, "mssql 17") {
		t.Errorf("message = %q, want the year rather than the raw major", msg)
	}
}

// Warn, never refuse: an unknown engine or an undetectable version must not
// stop a read-only command against someone's old production server.
func TestCheck_SaysNothingWhenItCannotTell(t *testing.T) {
	quiet := [][2]string{
		{"sqlite", "3"},
		{"postgres", ""},
		{"unknown-engine", "42"},
	}
	for _, c := range quiet {
		if ok, msg := Check(c[0], c[1]); !ok || msg != "" {
			t.Errorf("Check(%q, %q) = (%v, %q), want a silent pass", c[0], c[1], ok, msg)
		}
	}
}
