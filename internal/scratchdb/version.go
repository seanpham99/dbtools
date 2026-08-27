package scratchdb

import (
	"database/sql"
	"strings"
)

// ServerMajor returns the major version of the server behind db, as a bare
// string ("16", "17"), or "" when it cannot be determined.
//
// Callers use it to pin a scratch database to the same major version as the
// target it will be compared against. It is deliberately best-effort: an
// unknown version falls back to the default image, which is no worse than
// the behaviour before it existed. It never returns an error, because
// failing a read-only diff over a version probe would be a worse outcome
// than comparing against a default-major scratch database.
func ServerMajor(db *sql.DB, engineName string) string {
	switch engineName {
	case "postgres":
		// server_version_num is an integer like 160014 (16.14) or 90624
		// (9.6.24). Dividing by 10000 gives the major for 10+, which is
		// every version dbtools supports; 9.x would give "9", which is
		// not a usable image tag on its own and is filtered out by the
		// caller's plausibility check anyway.
		var num int
		if err := db.QueryRow("SHOW server_version_num").Scan(&num); err != nil || num <= 0 {
			return ""
		}
		return itoa(num / 10000)
	case "mysql":
		// VERSION() is like "8.0.36" or "8.4.2"; the image tag that
		// matters is the leading major.
		var v string
		if err := db.QueryRow("SELECT VERSION()").Scan(&v); err != nil {
			return ""
		}
		if i := strings.IndexByte(v, '.'); i > 0 {
			return v[:i]
		}
		return ""
	default:
		// mssql and sqlite: no major-version-shaped image tag to pin to.
		return ""
	}
}

// itoa avoids pulling strconv in for one small positive integer.
func itoa(n int) string {
	if n <= 0 {
		return ""
	}
	var b [4]byte
	i := len(b)
	for n > 0 && i > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
