package cmd

import (
	"testing"

	"github.com/seanpham99/dbtools/internal/adopt"
)

func TestAdoptAllowOrphansBefore(t *testing.T) {
	orphans := []uint64{19990101000000, 19990202000000}
	baseline := uint64(20260101000000)

	if !adopt.OrphansBelow(orphans, baseline) {
		t.Fatalf("OrphansBelow(%v, %d) = false, want true", orphans, baseline)
	}

	orphansWithEqual := []uint64{19990101000000, 20260101000000}
	if adopt.OrphansBelow(orphansWithEqual, baseline) {
		t.Fatalf("OrphansBelow(%v, %d) = true, want false when an orphan == baseline", orphansWithEqual, baseline)
	}

	orphansAbove := []uint64{19990101000000, 20270101000000}
	if adopt.OrphansBelow(orphansAbove, baseline) {
		t.Fatalf("OrphansBelow(%v, %d) = true, want false when an orphan > baseline", orphansAbove, baseline)
	}
}
