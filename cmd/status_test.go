package cmd

import (
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/statusinfo"
)

func TestRenderStatusTable(t *testing.T) {
	results := []statusinfo.TargetResult{
		{
			Target: "local",
			Status: &statusinfo.Status{
				Target:         "local",
				CurrentVersion: 20260101000000,
				HasVersion:     true,
				Dirty:          false,
				Pending:        nil,
			},
		},
		{
			Target:       "prod",
			Unconfigured: true,
		},
		{
			Target: "staging",
			Status: &statusinfo.Status{
				Target:         "staging",
				CurrentVersion: 20260101000000,
				HasVersion:     true,
				Dirty:          true,
				Pending:        []string{"20260102000000_add.up.sql"},
			},
		},
	}

	out := renderStatusTable(results)

	if !strings.Contains(out, "local       up to date") {
		t.Errorf("rendered output missing local status: %s", out)
	}
	if !strings.Contains(out, "prod        [unconfigured]") {
		t.Errorf("rendered output missing unconfigured prod status: %s", out)
	}
	if !strings.Contains(out, "staging     1 pending [DIRTY]") {
		t.Errorf("rendered output missing dirty staging status: %s", out)
	}
}
