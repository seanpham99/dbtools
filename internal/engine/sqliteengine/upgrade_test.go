package sqliteengine

import (
	"testing"

	"github.com/seanpham99/dbtools/internal/ledger"
)

// A ledger created before v0.7 constrains status to ('applied',
// 'reverted'). v0.7 writes "applying" before running each migration, so
// without widening that constraint the first run against an upgraded
// database fails on its own bookkeeping — and it fails *after* taking the
// migration lock, on a database the user has just been told is fine.
func TestEnsureSchema_WidensPreV07StatusConstraint(t *testing.T) {
	db := openTemp(t)
	const table = "dbtools_migration_history"

	// Exactly the pre-v0.7 shape.
	if _, err := db.Exec(`
CREATE TABLE dbtools_migration_history (
    version         INTEGER NOT NULL PRIMARY KEY,
    status          TEXT    NOT NULL CHECK (status IN ('applied', 'reverted')),
    recorded_at     TIMESTAMP NULL,
    note            TEXT    NULL,
    content_sha256  TEXT    NULL,
    hash_source     TEXT    NULL
)`); err != nil {
		t.Fatal(err)
	}
	store := ledgerStore{}
	if err := store.SetStatus(db, 20260101000000, ledger.StatusApplied, "from v0.6", table); err != nil {
		t.Fatalf("seeding a v0.6 row: %v", err)
	}

	if err := store.EnsureSchema(db, table); err != nil {
		t.Fatalf("EnsureSchema on a pre-v0.7 ledger: %v", err)
	}

	// The upgrade must not lose history.
	entries, err := store.List(db, table)
	if err != nil {
		t.Fatalf("List after upgrade: %v", err)
	}
	if len(entries) != 1 || entries[0].Version != 20260101000000 || entries[0].Note != "from v0.6" {
		t.Fatalf("entries after upgrade = %+v, want the seeded v0.6 row preserved", entries)
	}

	// And "applying" must now be writable, which is the whole point.
	if err := store.SetStatus(db, 20260102000000, ledger.StatusApplying, "mid-apply", table); err != nil {
		t.Fatalf("writing an applying row after upgrade: %v", err)
	}
	state, err := store.State(db, table)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Dirty || state.Applying != 20260102000000 {
		t.Errorf("state = %+v, want dirty at 20260102000000", state)
	}
	if !state.HasVersion || state.Version != 20260101000000 {
		t.Errorf("state = %+v, want the v0.6 row still counted as applied", state)
	}
}

// Running it twice must be a no-op, since EnsureSchema runs on every
// command.
func TestEnsureSchema_WidenIsIdempotent(t *testing.T) {
	db := openTemp(t)
	const table = "dbtools_migration_history"
	store := ledgerStore{}

	for i := 0; i < 3; i++ {
		if err := store.EnsureSchema(db, table); err != nil {
			t.Fatalf("EnsureSchema call %d: %v", i+1, err)
		}
	}
	if err := store.SetStatus(db, 1, ledger.StatusApplying, "", table); err != nil {
		t.Fatalf("writing an applying row: %v", err)
	}
}
