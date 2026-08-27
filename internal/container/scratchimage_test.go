package container

import "testing"

// A scratch database on a different version than the target renders catalog
// metadata differently, which diff reports as drift that does not exist.
// Parity is the mechanism that makes byte-exact comparison correct, so the
// mapping from a detected version to an image is load-bearing.
func TestScratchImageFor(t *testing.T) {
	cases := []struct {
		engine, series, want string
	}{
		{"postgres", "15", "postgres:15-alpine"},
		{"postgres", "18", "postgres:18-alpine"},
		// 8.0 and 8.4 are separate tag series that render differently, so
		// the major alone is not enough to pin MySQL.
		{"mysql", "8.0", "mysql:8.0"},
		{"mysql", "8.4", "mysql:8.4"},
		// SQL Server tags are years, not versions, so the mapping is
		// explicit rather than interpolated.
		{"mssql", "15", "mcr.microsoft.com/mssql/server:2019-latest"},
		{"mssql", "16", "mcr.microsoft.com/mssql/server:2022-latest"},
		{"mssql", "17", "mcr.microsoft.com/mssql/server:2025-latest"},
		// Unknown SQL Server major: no guess, fall back to the default.
		{"mssql", "14", ""},
		// No version detected: caller falls back to the default image.
		{"postgres", "", ""},
		{"sqlite", "3", ""},
	}
	for _, c := range cases {
		if got := ScratchImageFor(c.engine, c.series); got != c.want {
			t.Errorf("ScratchImageFor(%q, %q) = %q, want %q", c.engine, c.series, got, c.want)
		}
	}
}

// The series is interpolated into a docker image reference, so anything not
// version-shaped must be refused rather than passed through.
func TestPlausibleSeries(t *testing.T) {
	bad := []string{
		"", "latest", "16-alpine", "../evil", "1a", " 16",
		"16.14.1", // not a tag series, and too long
		".8", "8.", "8..0", "12345",
	}
	for _, v := range bad {
		if plausibleSeries(v) {
			t.Errorf("plausibleSeries(%q) = true, want false", v)
		}
	}
	for _, v := range []string{"15", "16", "17", "18", "8.0", "8.4", "9"} {
		if !plausibleSeries(v) {
			t.Errorf("plausibleSeries(%q) = false, want true", v)
		}
	}
}

// The default image for every engine must sit inside the supported window
// declared in docs/adr/003-v0.7-native-runner.md, so a user who cannot be
// pinned still lands on a supported version. MSSQL 2025 is outside it.
func TestDefaultImagesAreInsideTheSupportedWindow(t *testing.T) {
	cases := map[string]string{
		"mssql":    "mcr.microsoft.com/mssql/server:2022-latest",
		"postgres": "postgres:17-alpine",
	}
	for engineName, want := range cases {
		s, err := specFor(engineName)
		if err != nil {
			t.Fatalf("specFor(%q): %v", engineName, err)
		}
		if s.image != want {
			t.Errorf("%s default image = %q, want %q", engineName, s.image, want)
		}
	}
}
