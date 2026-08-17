package dbconn

import (
	"net/url"
	"testing"
)

func TestRewriteToSQLServerScheme(t *testing.T) {
	cases := []struct {
		name     string
		rawURL   string
		wantUser string
		wantPass string
		wantHost string
		wantPort string
	}{
		{
			name:     "simple",
			rawURL:   "mssql://sa:pw@localhost:14330?database=dbtools_local",
			wantUser: "sa",
			wantPass: "pw",
			wantHost: "localhost",
			wantPort: "14330",
		},
		{
			// Azure SQL DSNs can have "@" characters embedded in both the
			// username and password — confirm the last "@" before the host
			// still correctly separates userinfo from host, matching what
			// golang-migrate's own sqlserver driver already relies on.
			name:     "embedded at-signs in username and password",
			rawURL:   "mssql://appuser@tenant@sqlserverhost:p@ssw0rd@example.database.windows.net:1433?database=exampledb",
			wantUser: "appuser@tenant@sqlserverhost",
			wantPass: "p@ssw0rd",
			wantHost: "example.database.windows.net",
			wantPort: "1433",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := RewriteToSQLServerScheme(tc.rawURL)
			if err != nil {
				t.Fatalf("RewriteToSQLServerScheme() returned error: %v", err)
			}
			u, err := url.Parse(got)
			if err != nil {
				t.Fatalf("re-parsing rewritten URL %q: %v", got, err)
			}
			if u.Scheme != "sqlserver" {
				t.Errorf("Scheme = %q, want %q", u.Scheme, "sqlserver")
			}
			if u.Hostname() != tc.wantHost {
				t.Errorf("Hostname() = %q, want %q", u.Hostname(), tc.wantHost)
			}
			if u.Port() != tc.wantPort {
				t.Errorf("Port() = %q, want %q", u.Port(), tc.wantPort)
			}
			if got := u.User.Username(); got != tc.wantUser {
				t.Errorf("Username() = %q, want %q", got, tc.wantUser)
			}
			pw, _ := u.User.Password()
			if pw != tc.wantPass {
				t.Errorf("Password() = %q, want %q", pw, tc.wantPass)
			}
		})
	}
}

func TestRewriteToSQLServerScheme_InvalidURL(t *testing.T) {
	if _, err := RewriteToSQLServerScheme("://not-a-valid-url"); err == nil {
		t.Fatal("expected error for invalid URL, got nil")
	}
}
