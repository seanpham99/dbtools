package scratchdb

import (
	"database/sql"
	"strings"
)

// ServerSeries returns the version component that identifies which image
// series a scratch container has to run to render catalog metadata the same
// way db does. It is not always the major version, because "same rendering"
// does not always mean "same major":
//
//	postgres  major       "16"    image tags are major-only
//	mysql     major.minor "8.0"   8.0 and 8.4 render differently and ship as
//	                              separate tag series, so the major alone is
//	                              not enough to pin
//	mssql     major       "16"    mapped to a year tag by container.ScratchImageFor
//	sqlite    ""                  no server, nothing to match
//
// Returns "" when the version cannot be determined. Deliberately
// best-effort and error-free: callers fall back to the engine's default
// image and warn, because failing a read-only command over a version probe
// would be a worse outcome than comparing against a default-series scratch
// database.
func ServerSeries(db *sql.DB, engineName string) string {
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
		// VERSION() is like "8.0.36" or "8.4.2". Both components matter:
		// mysql:8.0 and mysql:8.4 are different images that render
		// expression defaults and CHECK clauses differently.
		var v string
		if err := db.QueryRow("SELECT VERSION()").Scan(&v); err != nil {
			return ""
		}
		return majorMinor(v)
	case "mssql":
		// ProductMajorVersion is 15 (2019), 16 (2022), 17 (2025). The
		// image tag is a year, so container.ScratchImageFor maps it —
		// this returns the major, not the tag.
		var v string
		if err := db.QueryRow("SELECT CONVERT(varchar, SERVERPROPERTY('ProductMajorVersion'))").Scan(&v); err != nil {
			return ""
		}
		return strings.TrimSpace(v)
	default:
		// sqlite: no server.
		return ""
	}
}

// majorMinor returns the first two dot-separated components of v ("8.0.36"
// -> "8.0"), or "" if v does not have two.
func majorMinor(v string) string {
	first := strings.IndexByte(v, '.')
	if first <= 0 {
		return ""
	}
	rest := v[first+1:]
	second := strings.IndexByte(rest, '.')
	if second < 0 {
		// "8.4" with no patch component is already major.minor.
		if rest == "" {
			return ""
		}
		return v
	}
	return v[:first+1+second]
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
