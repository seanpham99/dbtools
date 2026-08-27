package postgresengine

import (
	"math"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/ledger"
)

func TestSetStatusRejectsVersionAboveBigintRange(t *testing.T) {
	err := ledgerStore{}.SetStatus(nil, math.MaxInt64+1, ledger.StatusApplied, "", "dbtools_migration_history")
	if err == nil || !strings.Contains(err.Error(), "BIGINT range") {
		t.Fatalf("SetStatus(MaxInt64+1) err = %v, want BIGINT range error", err)
	}
}

func TestSetStatusAdoptedRejectsVersionAboveBigintRange(t *testing.T) {
	err := ledgerStore{}.SetStatusAdopted(nil, math.MaxInt64+1, "", "", "dbtools_migration_history")
	if err == nil || !strings.Contains(err.Error(), "BIGINT range") {
		t.Fatalf("SetStatusAdopted(MaxInt64+1) err = %v, want BIGINT range error", err)
	}
}
