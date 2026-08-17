package scaffold

import (
	"testing"
	"time"
)

func TestUpFilename(t *testing.T) {
	now := time.Date(2026, 7, 1, 4, 11, 34, 0, time.UTC)
	got := UpFilename(now, "add widget table")
	want := "20260701041134_add_widget_table.up.sql"
	if got != want {
		t.Errorf("UpFilename() = %q, want %q", got, want)
	}
}

func TestUpFilename_AlreadySlug(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	got := UpFilename(now, "add_widget_table")
	want := "20260102030405_add_widget_table.up.sql"
	if got != want {
		t.Errorf("UpFilename() = %q, want %q", got, want)
	}
}
