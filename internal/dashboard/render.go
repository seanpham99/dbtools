package dashboard

import (
	"fmt"

	"github.com/charmbracelet/bubbles/table"
)

func RenderRowStatus(r Row) string {
	if r.Err != nil {
		return "unreachable: " + r.Err.Error()
	}
	if n := len(r.Status.Pending); n > 0 {
		return fmt.Sprintf("%d pending", n)
	}
	return "up to date"
}

func ToTableRows(rows []Row) []table.Row {
	out := make([]table.Row, 0, len(rows))
	for _, r := range rows {
		out = append(out, table.Row{r.Target, RenderRowStatus(r)})
	}
	return out
}
