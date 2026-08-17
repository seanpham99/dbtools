package scaffold

import (
	"strings"
	"time"
)

// UpFilename returns the filename for a new migration created at `now`
// with the given human-readable name, e.g. "20260701041134_add_widget.up.sql".
func UpFilename(now time.Time, name string) string {
	slug := strings.ReplaceAll(strings.TrimSpace(name), " ", "_")
	return now.Format("20060102150405") + "_" + slug + ".up.sql"
}
