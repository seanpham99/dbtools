package migrator

import "testing"

func TestSchemeOf(t *testing.T) {
	cases := []struct{ in, want string }{
		{"mysql://root:pw@tcp(127.0.0.1:3306)/db", "mysql"},
		{"postgres://u:p@h:5432/db?sslmode=disable", "postgres"},
		{"sqlite://relative/to.db", "sqlite"},
		{"not-a-url-at-all", ""},
	}
	for _, c := range cases {
		if got := SchemeOf(c.in); got != c.want {
			t.Errorf("SchemeOf(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEnsureMySQLMultiStatements(t *testing.T) {
	cases := []struct{ in, want string }{
		{"mysql://root:pw@tcp(127.0.0.1:3306)/db", "mysql://root:pw@tcp(127.0.0.1:3306)/db?multiStatements=true"},
		{"mysql://root:pw@tcp(h:3306)/db?parseTime=true", "mysql://root:pw@tcp(h:3306)/db?parseTime=true&multiStatements=true"},
		{"mysql://root:pw@tcp(h:3306)/db?multiStatements=true", "mysql://root:pw@tcp(h:3306)/db?multiStatements=true"},
	}
	for _, c := range cases {
		if got := ensureMySQLMultiStatements(c.in); got != c.want {
			t.Errorf("ensureMySQLMultiStatements(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
