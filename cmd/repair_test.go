package cmd

import (
	"testing"

	"github.com/seanpham99/dbtools/internal/ledger"
	"github.com/seanpham99/dbtools/internal/repair"
)

func TestParseRepairArgs_Single(t *testing.T) {
	got, err := parseRepairArgs("20260101000000:applied")
	if err != nil {
		t.Fatalf("parseRepairArgs() returned error: %v", err)
	}
	want := repair.Pair{Version: 20260101000000, Status: ledger.StatusApplied}
	if len(got) != 1 || got[0] != want {
		t.Errorf("parseRepairArgs() = %+v, want [%+v]", got, want)
	}
}

func TestParseRepairArgs_Multiple(t *testing.T) {
	got, err := parseRepairArgs("20260101000000:applied,20260102000000:reverted")
	if err != nil {
		t.Fatalf("parseRepairArgs() returned error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("parseRepairArgs() returned %d pairs, want 2", len(got))
	}
	if got[0].Version != 20260101000000 || got[0].Status != ledger.StatusApplied {
		t.Errorf("pair[0] = %+v, want version=20260101000000 status=applied", got[0])
	}
	if got[1].Version != 20260102000000 || got[1].Status != ledger.StatusReverted {
		t.Errorf("pair[1] = %+v, want version=20260102000000 status=reverted", got[1])
	}
}

func TestParseRepairArgs_InvalidStatus(t *testing.T) {
	if _, err := parseRepairArgs("20260101000000:maybe"); err == nil {
		t.Fatal("expected error for invalid status, got nil")
	}
}

func TestParseRepairArgs_InvalidVersion(t *testing.T) {
	if _, err := parseRepairArgs("not-a-number:applied"); err == nil {
		t.Fatal("expected error for invalid version, got nil")
	}
}

func TestParseRepairArgs_MissingColon(t *testing.T) {
	if _, err := parseRepairArgs("20260101000000"); err == nil {
		t.Fatal("expected error for pair missing ':', got nil")
	}
}

func TestParseRepairArgs_DuplicateVersion(t *testing.T) {
	if _, err := parseRepairArgs("20260101000000:applied,20260101000000:reverted"); err == nil {
		t.Fatal("expected error for duplicate version, got nil")
	}
}
