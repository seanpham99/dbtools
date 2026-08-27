package dburl

import "testing"

// lib/pq validates unknown query parameters as server settings, so a URL
// still carrying golang-migrate's x- options fails to connect with
// `unrecognized configuration parameter "x-migrations-table"`.
func TestStripCustomParams(t *testing.T) {
	cases := map[string]string{
		"postgres://u:p@h:5432/db?sslmode=disable&x-migrations-table=t": "postgres://u:p@h:5432/db?sslmode=disable",
		"postgres://u:p@h:5432/db?x-migrations-table=t":                 "postgres://u:p@h:5432/db",
		"postgres://u:p@h:5432/db?sslmode=disable":                      "postgres://u:p@h:5432/db?sslmode=disable",
		"postgres://u:p@h:5432/db":                                      "postgres://u:p@h:5432/db",
	}
	for in, want := range cases {
		if got := StripCustomParams(in); got != want {
			t.Errorf("StripCustomParams(%q) = %q, want %q", in, got, want)
		}
	}
}

// MySQL DSNs use tcp(host:port), which is not an RFC 3986 URL. Returning
// them unchanged is correct — the mysql driver owns its own parameters.
func TestStripCustomParams_LeavesUnparseableUnchanged(t *testing.T) {
	in := "mysql://user:pass@tcp(127.0.0.1:3306)/db?x-migrations-table=t"
	if got := StripCustomParams(in); got != in {
		t.Errorf("StripCustomParams(%q) = %q, want it unchanged", in, got)
	}
}
