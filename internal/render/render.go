package render

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/seanpham99/dbtools/internal/statusinfo"
)

// JSON renders statuses as a compact JSON array for --json / agent callers.
func JSON(statuses []statusinfo.Status) (string, error) {
	b, err := json.Marshal(statuses)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Table renders statuses as a human-readable table for the plain CLI and,
// later, the TUI dashboard.
func Table(statuses []statusinfo.Status) string {
	var b strings.Builder
	for _, s := range statuses {
		state := "up to date"
		if n := len(s.Pending); n > 0 {
			state = fmt.Sprintf("%d pending", n)
		}
		dirtyMark := ""
		if s.Dirty {
			dirtyMark = " [DIRTY]"
		}
		fmt.Fprintf(&b, "%-10s  %s%s\n", s.Target, state, dirtyMark)
	}
	return b.String()
}
