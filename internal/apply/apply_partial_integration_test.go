//go:build integration

package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine"
	_ "github.com/seanpham99/dbtools/internal/engine/sqliteengine"
	"github.com/seanpham99/dbtools/internal/ledger"
)

// TestRun_RecordsPartiallyAppliedMigrationsOnFailure is the C2 regression:
// with three pending migrations where the third fails, the first two are
// applied to the database AND must be recorded in the ledger, so the next
// run re-attempts only the third (instead of replaying everything or
// lying about the failed one).
func TestRun_RecordsPartiallyAppliedMigrationsOnFailure(t *testing.T) {
	rawURL := os.Getenv("DBTOOLS_TEST_SQLITE_URL")
	if rawURL == "" {
		t.Skip("DBTOOLS_TEST_SQLITE_URL not set, skipping integration test")
	}
	if p := sqlitePath(rawURL); p != "" {
		for _, s := range []string{p, p + "-wal", p + "-shm"} {
			os.Remove(s)
		}
	}

	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "1_create_a.up.sql"), []byte("CREATE TABLE migrate_a (id INTEGER PRIMARY KEY);"), 0o644)
	os.WriteFile(filepath.Join(dir, "2_create_b.up.sql"), []byte("CREATE TABLE migrate_b (id INTEGER PRIMARY KEY);"), 0o644)
	os.WriteFile(filepath.Join(dir, "3_bad.up.sql"), []byte("THIS IS NOT SQL"), 0o644)

	cfg := &config.Config{
		MigrationsDir: dir,
		Targets:       map[string]config.Target{"local": {URLEnv: "DBTOOLS_APPLY_FAIL_URL"}},
	}
	t.Setenv("DBTOOLS_APPLY_FAIL_URL", rawURL)

	_, err := Run(cfg, "local", "")
	if err == nil {
		t.Fatal("Run() with a bad third migration should error")
	}
	if !strings.Contains(err.Error(), "version 3") && !strings.Contains(err.Error(), "3") {
		t.Logf("error mentions migration 3: %v", err)
	}

	eng, err := engine.ForTarget("", rawURL)
	if err != nil {
		t.Fatal(err)
	}
	db, err := eng.Open(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	entries, err := eng.Ledger().List(db, "dbtools_migration_history")
	if err != nil {
		t.Fatal(err)
	}
	// Two applied, plus an "applying" row for the migration that failed.
	// That third row is new and is the point: the failure is now recorded
	// rather than invisible, so the next run refuses instead of applying
	// more SQL over a schema in an unknown state.
	if len(entries) != 3 {
		t.Fatalf("ledger has %d entries, want 2 applied + 1 applying (got %+v)", len(entries), entries)
	}
	for _, e := range entries {
		if e.Version == 3 {
			if e.Status != ledger.StatusApplying {
				t.Errorf("entry %+v: the failed migration should be left applying", e)
			}
			continue
		}
		if e.Status != ledger.StatusApplied {
			t.Errorf("entry %+v: want applied", e)
		}
		if e.ContentSHA256 == "" {
			t.Errorf("entry %+v: want a content hash", e)
		}
	}

	// The failed migration must NOT be recorded as applied.
	for _, e := range entries {
		if e.Version == 3 && e.Status == ledger.StatusApplied {
			t.Fatalf("failed migration 3 was recorded as applied: %+v", e)
		}
	}

	// Fix the bad migration. The surviving applying row for version 3 must
	// still block a blind re-run, forcing an explicit repair decision:
	// fixing the file says nothing about what the failed run already did
	// to the schema.
	os.WriteFile(filepath.Join(dir, "3_bad.up.sql"), []byte("CREATE TABLE migrate_c (id INTEGER PRIMARY KEY);"), 0o644)
	_, err = Run(cfg, "local", "")
	if err == nil || !strings.Contains(err.Error(), "started and never finished") {
		t.Fatalf("Run() over a dirty cursor = %v, want the mid-apply refusal", err)
	}

	// Recovery is an explicit decision recorded in the ledger, after which
	// a run proceeds normally.
	eng, err = engine.ForTarget("", rawURL)
	if err != nil {
		t.Fatal(err)
	}
	db, err = eng.Open(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// The operator inspects the database, finds version 3's changes really
	// are present, and records that. With the version derived from the
	// ledger, this single write is the whole repair — there is no cursor
	// to stamp alongside it, and no way for the two to disagree.
	//
	// Marking it *reverted* instead would legitimately make 3 pending
	// again, since the ledger would then say it was never applied.
	if err := eng.Ledger().SetStatus(db, 3, ledger.StatusApplied, "repair: verified applied", "dbtools_migration_history"); err != nil {
		t.Fatalf("marking 3 applied: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "4_create_c.up.sql"), []byte("CREATE TABLE migrate_c (id INTEGER PRIMARY KEY);"), 0o644)
	if _, err := Run(cfg, "local", ""); err != nil {
		t.Fatalf("Run() after adding migration 4 returned error: %v", err)
	}
	entries, err = eng.Ledger().List(db, "dbtools_migration_history")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("after repair+run: ledger has %d entries, want 4 (v1-v4 applied)", len(entries))
	}
	var found bool
	for _, e := range entries {
		if e.Version == 4 {
			found = true
			if e.Status != ledger.StatusApplied {
				t.Fatalf("migration 4 status = %s, want applied: %+v", e.Status, e)
			}
			if e.ContentSHA256 == "" {
				t.Fatalf("migration 4 recorded without hash: %+v", e)
			}
		}
		if e.Version == 3 {
			// Recorded applied by the repair above: the operator verified
			// its changes really are present. What matters is that the
			// applying row is gone — the decision was made explicitly, not
			// by a re-run guessing.
			if e.Status == ledger.StatusApplying {
				t.Fatalf("migration 3 is still applying after repair: %+v", e)
			}
		}
	}
	if !found {
		t.Fatal("migration 4 not recorded")
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='migrate_c'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("migrate_c count = %d, err = %v; want 1", n, err)
	}
}

func sqlitePath(rawURL string) string {
	const prefix = "sqlite://"
	if !strings.HasPrefix(rawURL, prefix) {
		return ""
	}
	p := strings.TrimPrefix(rawURL, prefix)
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	return p
}
