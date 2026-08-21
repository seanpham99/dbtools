package cmd

import (
	"testing"

	"github.com/seanpham99/dbtools/internal/statusinfo"
)

func TestBuildStatusEntries_MixedSuccessAndFailure(t *testing.T) {
	statuses := []statusinfo.Status{
		{Target: "local", CurrentVersion: 20260101000000, HasVersion: true, Dirty: false, Pending: nil},
	}
	failures := []targetFailure{
		{Target: "prod", Error: `environment variable DBTOOLS_PROD_URL is not set`},
	}

	got := buildStatusEntries(statuses, failures)

	if len(got) != 2 {
		t.Fatalf("buildStatusEntries() returned %d entries, want 2", len(got))
	}
	if got[0].Target != "local" || got[0].CurrentVersion != 20260101000000 || got[0].Error != "" {
		t.Errorf("entry[0] = %+v, want a successful local entry with no error", got[0])
	}
	if got[1].Target != "prod" || got[1].Error == "" {
		t.Errorf("entry[1] = %+v, want a failed prod entry with a non-empty error", got[1])
	}
}

func TestBuildStatusEntries_AllFailed(t *testing.T) {
	failures := []targetFailure{
		{Target: "staging", Error: "boom"},
	}

	got := buildStatusEntries(nil, failures)

	if len(got) != 1 {
		t.Fatalf("buildStatusEntries() returned %d entries, want 1", len(got))
	}
	if got[0].Target != "staging" || got[0].Error != "boom" {
		t.Errorf("entry[0] = %+v, want {Target: staging, Error: boom}", got[0])
	}
}
