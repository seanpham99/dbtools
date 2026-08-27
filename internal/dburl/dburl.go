// Package dburl manipulates database connection URLs around
// golang-migrate's custom "x-" query parameters.
//
// Connection URLs sometimes carry migration-tool options in their query
// string — x-migrations-table and friends, a golang-migrate convention that
// predates dbtools owning its own runner. Database drivers do not know
// those options and reject them outright:
//
//	pq: unrecognized configuration parameter "x-migrations-table"
//
// dbtools no longer produces them, but a URL held in a CI secret or a vault
// may still contain one, and failing a connection over a parameter dbtools
// itself ignores would be a poor upgrade. So they are stripped before the
// URL reaches a driver.
package dburl

import (
	"net/url"
	"strings"
)

// CustomParamPrefix is golang-migrate's namespace for its own options.
const CustomParamPrefix = "x-"

// StripCustomParams returns rawURL without any x- query parameters, so it
// is safe to hand to a database driver. URLs it cannot parse are returned
// unchanged: MySQL DSNs in tcp(host:port) form are not RFC 3986 URLs, and
// the mysql driver handles its own parameters anyway.
func StripCustomParams(rawURL string) string {
	if !strings.Contains(rawURL, CustomParamPrefix) {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	stripped := false
	for k := range q {
		if strings.HasPrefix(strings.ToLower(k), CustomParamPrefix) {
			q.Del(k)
			stripped = true
		}
	}
	if !stripped {
		return rawURL
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// SchemeOf returns rawURL's scheme. It first tries a plain string cut on
// "://" because several dbtools connection strings are not valid net/url
// URLs — MySQL's mysql://user:pass@tcp(host:port)/db, for instance, makes
// url.Parse fail outright ("invalid port") on the tcp(...) host syntax.
// Falls back to url.Parse for anything that isn't scheme-prefixed that way,
// returning "" if neither yields a scheme.
//
// This lives here rather than in internal/migrator so that engine
// resolution does not depend on the migration runner: the runner needs to
// call into engines, and the reverse edge would make that a cycle.
func SchemeOf(rawURL string) string {
	if idx := strings.Index(rawURL, "://"); idx > 0 {
		return rawURL[:idx]
	}
	if u, err := url.Parse(rawURL); err == nil {
		return u.Scheme
	}
	return ""
}
