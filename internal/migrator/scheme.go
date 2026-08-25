package migrator

import (
	"net/url"
	"strings"
)

// SchemeOf returns rawURL's scheme. It first tries a plain string cut on
// "://" because several dbtools connection strings are not valid
// net/url URLs — MySQL's mysql://user:pass@tcp(host:port)/db, for
// instance, makes url.Parse fail outright ("invalid port") on the
// tcp(...) host syntax. Falls back to url.Parse for anything that isn't
// scheme-prefixed that way, returning "" if neither yields a scheme.
func SchemeOf(rawURL string) string {
	if idx := strings.Index(rawURL, "://"); idx > 0 {
		return rawURL[:idx]
	}
	if u, err := url.Parse(rawURL); err == nil {
		return u.Scheme
	}
	return ""
}

// ensureMySQLMultiStatements appends multiStatements=true to rawURL if
// not already present. golang-migrate's mysql driver executes each
// migration file as a single query; without this parameter MySQL
// silently runs only the first statement of a multi-statement file — no
// error, and the ledger still records the version applied — exactly the
// silent schema drift dbtools exists to prevent. Forced here so a target
// URL doesn't have to remember this query param, the same way
// mysqlengine.dsnFromURL forces parseTime=true for direct connections.
func ensureMySQLMultiStatements(rawURL string) string {
	if strings.Contains(rawURL, "multiStatements=") {
		return rawURL
	}
	sep := "?"
	if strings.Contains(rawURL, "?") {
		sep = "&"
	}
	return rawURL + sep + "multiStatements=true"
}
