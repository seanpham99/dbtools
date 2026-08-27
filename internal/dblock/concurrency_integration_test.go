//go:build integration

package dblock_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/seanpham99/dbtools/internal/dblock"
	"github.com/seanpham99/dbtools/internal/engine"

	_ "github.com/seanpham99/dbtools/internal/engine/mssqlengine"
	_ "github.com/seanpham99/dbtools/internal/engine/mysqlengine"
	_ "github.com/seanpham99/dbtools/internal/engine/postgresengine"
)

// The property that matters: while one run holds the migration lock, a
// second run cannot take it. Nothing about that is provable with a fake —
// the whole point is the server's own arbitration between two sessions — so
// this is an integration test against each real engine.
//
// Before this existed, dbtools implemented no locking at all: Lock/Unlock
// were pure delegation to golang-migrate, and had zero test coverage in
// this repo.
func TestIntegrationLockIsMutuallyExclusive(t *testing.T) {
	for _, tc := range []struct {
		engineName string
		envVar     string
	}{
		{"postgres", "DBTOOLS_TEST_POSTGRES_URL"},
		{"mysql", "DBTOOLS_TEST_MYSQL_URL"},
		{"mssql", "DBTOOLS_TEST_MSSQL_URL"},
	} {
		t.Run(tc.engineName, func(t *testing.T) {
			rawURL := os.Getenv(tc.envVar)
			if rawURL == "" {
				t.Skipf("%s not set, skipping", tc.envVar)
			}
			eng, err := engine.ForName(tc.engineName)
			if err != nil {
				t.Fatal(err)
			}
			acquire, release, err := dblock.ForEngine(tc.engineName)
			if err != nil {
				t.Fatal(err)
			}

			// Two independent pools stand in for two dbtools processes.
			first, err := eng.Open(rawURL)
			if err != nil {
				t.Fatal(err)
			}
			defer first.Close()
			second, err := eng.Open(rawURL)
			if err != nil {
				t.Fatal(err)
			}
			defer second.Close()

			ctx := context.Background()
			key := dblock.KeyFor("dblock-mutual-exclusion-test")

			held, err := dblock.Acquire(ctx, first, key, acquire, release)
			if err != nil {
				t.Fatalf("first Acquire: %v", err)
			}

			// The second acquisition must block. Give it a deadline: if it
			// returns success before the first is released, the lock does
			// not exclude and two runners could apply migrations together.
			blocked := make(chan error, 1)
			go func() {
				waitCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				defer cancel()
				l, err := dblock.Acquire(waitCtx, second, key, acquire, release)
				if err == nil {
					// Should not happen while the first is held; hand it back
					// so the test cannot wedge the shared server.
					_ = l.Release(ctx)
				}
				blocked <- err
			}()

			select {
			case err := <-blocked:
				if err == nil {
					t.Fatal("second Acquire succeeded while the lock was held — the lock does not exclude")
				}
				// Timed out waiting, which is the expected outcome.
			case <-time.After(5 * time.Second):
				t.Fatal("second Acquire neither returned nor timed out")
			}

			if err := held.Release(ctx); err != nil {
				t.Fatalf("Release: %v", err)
			}

			// Once released, the lock must be takeable again — a lock that
			// excludes forever is just as broken as one that never does.
			retakeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			retaken, err := dblock.Acquire(retakeCtx, second, key, acquire, release)
			if err != nil {
				t.Fatalf("Acquire after Release: %v — the lock was not handed back", err)
			}
			if err := retaken.Release(ctx); err != nil {
				t.Fatalf("Release of retaken lock: %v", err)
			}
		})
	}
}

// Releasing a lock this session does not hold must be reported, not
// swallowed: on Postgres the unlock is refcounted, so a silent double
// release would hand back a lock another run currently owns.
func TestIntegrationReleaseWithoutHoldingIsAnError(t *testing.T) {
	rawURL := os.Getenv("DBTOOLS_TEST_POSTGRES_URL")
	if rawURL == "" {
		t.Skip("DBTOOLS_TEST_POSTGRES_URL not set, skipping")
	}
	eng, err := engine.ForName("postgres")
	if err != nil {
		t.Fatal(err)
	}
	db, err := eng.Open(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, release, err := dblock.ForEngine("postgres")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	if err := release(ctx, conn, dblock.KeyFor("never-acquired")); err == nil {
		t.Error("releasing a lock that was never held should report it, not succeed silently")
	}
}
