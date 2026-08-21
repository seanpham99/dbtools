package render

import (
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/statusinfo"
)

func sample() []statusinfo.Status {
	return []statusinfo.Status{
		{Target: "local", CurrentVersion: 20260101000000, HasVersion: true, Dirty: false, Pending: nil},
		{Target: "staging", CurrentVersion: 0, HasVersion: false, Dirty: false, Pending: []string{"20260101000000_a.up.sql"}},
	}
}

func TestJSON(t *testing.T) {
	out, err := JSON(sample())
	if err != nil {
		t.Fatalf("JSON() returned error: %v", err)
	}
	if !strings.Contains(out, `"target":"local"`) {
		t.Errorf("JSON() missing local target: %s", out)
	}
	if !strings.Contains(out, `"pending":["20260101000000_a.up.sql"]`) {
		t.Errorf("JSON() missing staging pending list: %s", out)
	}
}

func TestTable(t *testing.T) {
	out := Table(sample())
	if !strings.Contains(out, "local") || !strings.Contains(out, "staging") {
		t.Errorf("Table() missing target names: %s", out)
	}
	if !strings.Contains(out, "1 pending") {
		t.Errorf("Table() missing pending count for staging: %s", out)
	}
	if !strings.Contains(out, "up to date") {
		t.Errorf("Table() missing up-to-date marker for local: %s", out)
	}
}
