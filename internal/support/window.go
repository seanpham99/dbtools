// Package support declares which database server versions dbtools is
// tested against, and reports when a target falls outside that window.
//
// The window exists because behaviour is not stable across majors —
// catalog rendering, DDL semantics and the statements a dump emits all
// move — and dbtools cannot claim correctness on a version its tests never
// touch. Declaring it makes the boundary visible instead of implied.
package support

import "fmt"

// Window is the set of server versions CI runs against for one engine.
//
// Versions holds the exact values scratchdb.ServerSeries produces, not the
// marketing name: SQL Server reports ProductMajorVersion (16), never the
// year (2022). Display carries the name a human expects to read.
//
// The set is enumerated rather than expressed as a range, because "between
// oldest and newest" is the wrong test for two of the three engines: MySQL
// 8.1-8.3 sit numerically between the 8.0 and 8.4 LTS lines without being
// either, and SQL Server majors do not map to the years anyone quotes.
type Window struct {
	Versions []string
	Display  string
}

// Windows maps an engine to the versions dbtools is tested against.
//
// Postgres starts at 15 rather than 14 because 14 leaves upstream support
// during this release's life. MySQL lists only the LTS series; the
// innovation releases between them are deliberately not claimed.
var Windows = map[string]Window{
	"postgres": {Versions: []string{"15", "16", "17", "18"}, Display: "15-18"},
	"mysql":    {Versions: []string{"8.0", "8.4"}, Display: "8.0 and 8.4"},
	// ProductMajorVersion 15 is SQL Server 2019; 16 is 2022.
	"mssql": {Versions: []string{"15", "16"}, Display: "2019 and 2022"},
}

// mssqlYears maps ProductMajorVersion to the release year SQL Server is
// known by, so a warning does not quote a version nobody recognises. Kept
// beside the window so the two cannot drift.
var mssqlYears = map[string]string{
	"13": "2016", "14": "2017", "15": "2019", "16": "2022", "17": "2025",
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
	for _, v := range w.Versions {
		if v == series {
			return true, ""
		}
	}
	return false, fmt.Sprintf(
		"%s %s is outside the tested range (%s); dbtools should still work, but version-specific "+
			"behaviour here is not covered by its test suite",
		engineName, Name(engineName, series), w.Display)
}

// Name renders a version the way its vendor does.
func Name(engineName, series string) string {
	if engineName == "mssql" {
		if year, ok := mssqlYears[series]; ok {
			return year
		}
	}
	return series
}
