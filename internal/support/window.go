// Package support declares which database server versions dbtools is
// tested against, and reports when a target falls outside that window.
//
// The window exists because behaviour is not stable across majors —
// catalog rendering, DDL semantics and the statements a dump emits all
// move — and dbtools cannot claim correctness on a version its tests never
// touch. Declaring it makes the boundary visible instead of implied.
package support

import "fmt"

// Window is the tested version range for one engine.
type Window struct {
	// Oldest and Newest are the majors CI runs against. Everything between
	// them is expected to work and is not individually tested; the
	// differences that matter cluster at the boundaries.
	Oldest string
	Newest string
	// Display renders the window for humans ("15-18").
	Display string
}

// Windows maps an engine to the versions dbtools is tested against.
//
// Postgres starts at 15 rather than 14 because 14 leaves upstream support
// during this release's life, and MySQL lists 8.0 and 8.4 because those are
// separate LTS series rather than two points on one.
var Windows = map[string]Window{
	"postgres": {Oldest: "15", Newest: "18", Display: "15-18"},
	"mysql":    {Oldest: "8.0", Newest: "8.4", Display: "8.0 and 8.4"},
	"mssql":    {Oldest: "2019", Newest: "2022", Display: "2019 and 2022"},
}

// Check reports whether series is inside engineName's tested window.
//
// An unknown engine, or a version that could not be determined, is not an
// error: dbtools proceeds and says so. Refusing to run a read-only check
// against an old production server would be worse than warning about it.
func Check(engineName, series string) (ok bool, message string) {
	w, known := Windows[engineName]
	if !known || series == "" {
		return true, ""
	}
	if inWindow(engineName, series, w) {
		return true, ""
	}
	return false, fmt.Sprintf(
		"%s %s is outside the tested range (%s); dbtools should still work, but version-specific "+
			"behaviour here is not covered by its test suite",
		engineName, series, w.Display)
}

// inWindow compares series against the window.
//
// Postgres majors are integers, MySQL series are major.minor, and SQL
// Server versions are years — all three happen to compare correctly as
// numbers when parsed leniently, so one comparison covers them.
func inWindow(engineName, series string, w Window) bool {
	v, ok := parse(series)
	if !ok {
		return true // unparseable: say nothing rather than guess
	}
	lo, loOK := parse(w.Oldest)
	hi, hiOK := parse(w.Newest)
	if !loOK || !hiOK {
		return true
	}
	return v >= lo && v <= hi
}

// parse turns "15", "8.0" or "2019" into a comparable number
// (15 -> 1500, 8.0 -> 800, 2019 -> 201900). Returns false for anything
// that is not version-shaped.
func parse(s string) (int, bool) {
	major, minor := 0, 0
	seenDot := false
	digits := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '.' {
			if seenDot || digits == 0 {
				return 0, false
			}
			seenDot = true
			digits = 0
			continue
		}
		if c < '0' || c > '9' {
			return 0, false
		}
		digits++
		if seenDot {
			minor = minor*10 + int(c-'0')
		} else {
			major = major*10 + int(c-'0')
		}
	}
	if digits == 0 {
		return 0, false
	}
	return major*100 + minor, true
}
