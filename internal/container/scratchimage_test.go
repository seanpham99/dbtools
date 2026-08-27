package container

import "testing"

// A scratch database on a different major than the target renders catalog
// metadata differently, which diff reports as drift that does not exist.
func TestScratchImageFor(t *testing.T) {
	cases := []struct {
		engine, major, want string
	}{
		{"postgres", "16", "postgres:16-alpine"},
		{"postgres", "17", "postgres:17-alpine"},
		{"mysql", "8", "mysql:8"},
		// No major detected: caller falls back to the spec's default image.
		{"postgres", "", ""},
		// mssql tags are not major-shaped (2022-latest), so don't guess.
		{"mssql", "16", ""},
		{"sqlite", "3", ""},
	}
	for _, c := range cases {
		if got := ScratchImageFor(c.engine, c.major); got != c.want {
			t.Errorf("ScratchImageFor(%q, %q) = %q, want %q", c.engine, c.major, got, c.want)
		}
	}
}

// The major is interpolated into a docker image reference, so anything that
// is not a plain one- or two-digit number must be refused rather than
// passed through.
func TestPlausibleMajor_RejectsNonNumeric(t *testing.T) {
	bad := []string{"", "16.14", "latest", "16-alpine", "../evil", "999", "1a", " 16"}
	for _, v := range bad {
		if plausibleMajor(v) {
			t.Errorf("plausibleMajor(%q) = true, want false", v)
		}
	}
	for _, v := range []string{"8", "16", "17", "18"} {
		if !plausibleMajor(v) {
			t.Errorf("plausibleMajor(%q) = false, want true", v)
		}
	}
}
