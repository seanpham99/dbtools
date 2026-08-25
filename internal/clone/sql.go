package clone

import (
	"fmt"
	"strings"
)

// placeholder returns the i'th (1-indexed) bound-parameter placeholder for
// engineName's driver. Clone requires source and dest to share an engine
// (see Run in run.go), so exactly one dialect's placeholder style is ever
// needed per invocation — there is no cross-dialect translation here, only
// a lookup by name.
func placeholder(engineName string, i int) string {
	switch engineName {
	case "mssql":
		return fmt.Sprintf("@p%d", i)
	case "postgres":
		return fmt.Sprintf("$%d", i)
	default: // sqlite, mysql
		return "?"
	}
}

// buildSelectSQL builds the source-side read query for one table.
// limit <= 0 means no row limit; where == "" means no filter. MSSQL has no
// LIMIT clause — it uses TOP N right after SELECT instead.
func buildSelectSQL(engineName, table string, limit int, where string) string {
	whereClause := ""
	if where != "" {
		whereClause = " WHERE " + where
	}
	if engineName == "mssql" {
		topClause := ""
		if limit > 0 {
			topClause = fmt.Sprintf("TOP %d ", limit)
		}
		return fmt.Sprintf("SELECT %s* FROM %s%s", topClause, table, whereClause)
	}
	limitClause := ""
	if limit > 0 {
		limitClause = fmt.Sprintf(" LIMIT %d", limit)
	}
	return fmt.Sprintf("SELECT * FROM %s%s%s", table, whereClause, limitClause)
}

// buildInsertSQL builds the dest-side write query for one table, with one
// bound placeholder per column in the same order columns is given.
func buildInsertSQL(engineName, table string, columns []string) string {
	placeholders := make([]string, len(columns))
	for i := range columns {
		placeholders[i] = placeholder(engineName, i+1)
	}
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(columns, ", "), strings.Join(placeholders, ", "))
}
