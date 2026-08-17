package postgresengine

import (
	"math"
	"strings"
	"testing"

	"github.com/dbtools/dbtools/internal/ledger"
)

func TestSetStatusRejectsVersionAboveBigintRange(t *testing.T) {
	err := ledgerStore{}.SetStatus(nil, math.MaxInt64+1, ledger.StatusApplied, "")
	if err == nil || !strings.Contains(err.Error(), "BIGINT range") {
		t.Fatalf("SetStatus(MaxInt64+1) err = %v, want BIGINT range error", err)
	}
}

func TestBackfillRejectsVersionAboveBigintRange(t *testing.T) {
	err := ledgerStore{}.backfill(nil, math.MaxUint64, true, []uint64{math.MaxInt64 + 1})
	if err == nil || !strings.Contains(err.Error(), "BIGINT range") {
		t.Fatalf("backfill(MaxInt64+1) err = %v, want BIGINT range error", err)
	}
}
