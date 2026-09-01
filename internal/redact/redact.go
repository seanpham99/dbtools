// Package redact strips passwords from connection strings before they
// appear in error messages or logs. Connection URLs resolve from
// environment variables (see internal/config) and carry credentials, so
// any error that embeds one must embed it redacted.
package redact

import (
	"errors"
	"net/url"
	"regexp"
)

// userinfoPasswordRE matches the password in both RFC 3986 URLs
// (postgres://user:pw@host/db) and non-RFC-3986 DSNs that net/url cannot
// parse (user:pw@tcp(host:port)/db). The character class stops the match
// at scheme separators, so ports like ":5432" without a following "@" are
// never touched.
var userinfoPasswordRE = regexp.MustCompile(`:([^:@/\s]+)@`)

// URL returns rawURL with its password replaced by "***". Strings
// without a password component are returned unchanged.
func URL(rawURL string) string {
	return userinfoPasswordRE.ReplaceAllString(rawURL, ":***@")
}

// ParseError returns err with any raw URL removed: net/url's *url.Error
// renders the full input (including the password) in its message, so
// callers wrapping parse failures must unwrap to the bare reason and
// name the URL themselves via URL().
func ParseError(err error) error {
	var uerr *url.Error
	if errors.As(err, &uerr) {
		return uerr.Err
	}
	return err
}
