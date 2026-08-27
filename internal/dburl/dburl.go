// Package dburl manipulates database connection URLs around
// golang-migrate's custom "x-" query parameters.
//
// golang-migrate reads its own options (x-migrations-table,
// x-statement-timeout, ...) from the connection URL's query string.
// Database drivers do not know those options and reject them:
// lib/pq fails a connection carrying x-migrations-table with
//
//	pq: unrecognized configuration parameter "x-migrations-table"
//
// so any code path that opens its own connection from a URL that
// golang-migrate also consumes has to strip them first. That is every
// dbtools engine Open, and it is why the parameter cannot simply be
// documented as a user-supplied workaround.
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

// WithParam returns rawURL with key=value set in its query string,
// replacing any existing value. A URL that cannot be parsed is returned
// unchanged rather than corrupted.
func WithParam(rawURL, key, value string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	q.Set(key, value)
	u.RawQuery = q.Encode()
	return u.String()
}

// Param returns the value of key in rawURL's query string, or "".
func Param(rawURL, key string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Query().Get(key)
}
