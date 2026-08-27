package migrator

import (
	"strings"

	"github.com/seanpham99/dbtools/internal/dburl"
)

// SchemeOf returns rawURL's scheme. Thin re-export of dburl.SchemeOf, kept
// so callers already reaching for the migrator package do not all have to
// change; the implementation lives in dburl because engine resolution needs
// it and must not depend on the runner.
func SchemeOf(rawURL string) string {
	return dburl.SchemeOf(rawURL)
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
