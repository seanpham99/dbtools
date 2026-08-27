package dblock

import (
	"context"
	"testing"
)

func TestKeyFor_ScopesToDatabase(t *testing.T) {
	if KeyFor("app") == KeyFor("other") {
		t.Error("different databases must not share a lock key — unrelated runs would serialise")
	}
	// Assigned first: comparing the two calls inline reads as a tautology
	// to staticcheck, but the property under test is that the function is
	// deterministic — two dbtools processes derive the key independently.
	first, second := KeyFor("app"), KeyFor("app")
	if first != second {
		t.Errorf("KeyFor is not stable (%q vs %q): two runs against one database must agree on the key", first, second)
	}
	if KeyFor("") == "" {
		t.Error("an unknown database name still needs a usable key")
	}
}

func TestNumericKey_StableAndDistinct(t *testing.T) {
	first, second := NumericKey("dbtools-app"), NumericKey("dbtools-app")
	if first != second {
		t.Fatalf("NumericKey is not stable (%d vs %d): two runs would take different locks", first, second)
	}
	if NumericKey("dbtools-app") == NumericKey("dbtools-other") {
		t.Error("distinct keys collided; unrelated databases would serialise")
	}
}

// A released lock must not release twice. On Postgres, pg_advisory_unlock is
// refcounted, so a double release would hand back a lock this run no longer
// owns — releasing someone else's.
func TestLock_ReleaseIsIdempotent(t *testing.T) {
	if err := (*Lock)(nil).Release(context.Background()); err != nil {
		t.Errorf("Release on a nil lock should be a no-op, got %v", err)
	}

	calls := 0
	l := &Lock{release: func(context.Context) error { calls++; return nil }}
	// conn is nil, which is the state Release leaves behind. Releasing again
	// must be a no-op rather than calling the releaser a second time.
	if err := l.Release(context.Background()); err != nil {
		t.Fatalf("Release on an already-released lock: %v", err)
	}
	if calls != 0 {
		t.Errorf("releaser called %d times on an already-released lock, want 0", calls)
	}
}
