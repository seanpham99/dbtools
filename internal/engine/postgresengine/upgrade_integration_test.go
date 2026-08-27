//go:build integration

package postgresengine

import (
	"os"
	"testing"

	"github.com/seanpham99/dbtools/internal/ledger"
)

// The upgrade path that matters for shipping v0.7: a ledger created by
// v0.6 constrains status to ('applied', 'reverted'), so the first
// migration run against it would fail writing the "applying" row — after
// taking the migration lock, on a database the user was just told is fine.
func TestIntegrationEnsureSchema_WidensPreV07StatusConstraint(t *testing.T) {
	rawURL := os.Getenv("DBTOOLS_TEST_POSTGRES_URL")
	if rawURL == "" {
		t.Skip("DBTOOLS_TEST_POSTGRES_URL not set, skipping integration test")
	}
	db, err := Postgres{}.Open(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	const table = "dbtools_v06_upgrade_it"
	t.Cleanup(func() { db.Exec(`DROP TABLE IF EXISTS ` + table) })
	if _, err := db.Exec(`DROP TABLE IF EXISTS ` + table); err != nil {
		t.Fatal(err)
	}
	// Exactly the pre-v0.7 shape, including the inline CHECK Postgres
	// auto-names "<table>_status_check".
	if _, err := db.Exec(`
CREATE TABLE ` + table + ` (
    version         BIGINT       NOT NULL PRIMARY KEY,
    status          VARCHAR(10)  NOT NULL CHECK (status IN ('applied', 'reverted')),
    recorded_at     TIMESTAMPTZ  NULL,
    note            VARCHAR(400) NULL,
    content_sha256  CHAR(64)     NULL,
    hash_source     VARCHAR(20)  NULL
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

	// History preserved, and "applying" now writable.
	if err := store.SetStatus(db, 20260102000000, ledger.StatusApplying, "mid-apply", table); err != nil {
		t.Fatalf("writing an applying row after upgrade: %v", err)
	}
	state, err := store.State(db, table)
	if err != nil {
		t.Fatal(err)
	}
	if !state.HasVersion || state.Version != 20260101000000 {
		t.Errorf("state = %+v, want the v0.6 row still applied", state)
	}
	if !state.Dirty || state.Applying != 20260102000000 {
		t.Errorf("state = %+v, want dirty at 20260102000000", state)
	}

	// Idempotent: EnsureSchema runs on every command.
	if err := store.EnsureSchema(db, table); err != nil {
		t.Fatalf("second EnsureSchema: %v", err)
	}
}
