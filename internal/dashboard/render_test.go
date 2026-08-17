package dashboard

import (
	"strings"
	"testing"

	"github.com/dbtools/dbtools/internal/statusinfo"
)

func TestRenderRowStatus_UpToDate(t *testing.T) {
	r := Row{Target: "local", Status: &statusinfo.Status{Target: "local", HasVersion: true, Pending: nil}}
	got := RenderRowStatus(r)
	if got != "up to date" {
		t.Fatalf("RenderRowStatus() = %q, want %q", got, "up to date")
	}
}

func TestRenderRowStatus_Pending(t *testing.T) {
	r := Row{Target: "local", Status: &statusinfo.Status{Target: "local", HasVersion: true, Pending: []string{"a", "b"}}}
	got := RenderRowStatus(r)
	if got != "2 pending" {
		t.Fatalf("RenderRowStatus() = %q, want %q", got, "2 pending")
	}
}

func TestRenderRowStatus_Error(t *testing.T) {
	r := Row{Target: "staging", Err: testErr{"staging"}}
	got := RenderRowStatus(r)
	if !strings.HasPrefix(got, "unreachable: ") {
		t.Fatalf("RenderRowStatus() = %q, want prefix %q", got, "unreachable: ")
	}
}

func TestToTableRows(t *testing.T) {
	rows := []Row{
		{Target: "local", Status: &statusinfo.Status{Target: "local", HasVersion: true, Pending: nil}},
		{Target: "staging", Err: testErr{"staging"}},
	}
	got := ToTableRows(rows)
	if len(got) != 2 {
		t.Fatalf("ToTableRows() returned %d rows, want 2", len(got))
	}
	if got[0][0] != "local" || got[1][0] != "staging" {
		t.Fatalf("ToTableRows() target column = %q, %q", got[0][0], got[1][0])
	}
	if got[0][1] != "up to date" {
		t.Fatalf("ToTableRows()[0][1] = %q, want %q", got[0][1], "up to date")
	}
}

type testErr struct{ target string }

func (e testErr) Error() string { return "environment variable for target " + e.target + " is not set" }
