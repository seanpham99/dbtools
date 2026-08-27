// Package dblock provides the cross-process mutual exclusion that keeps two
// dbtools runs from applying migrations to one database at the same time.
//
// This is the one guarantee dbtools cannot get slightly wrong and still be
// useful. Two concurrent runners interleaving DDL corrupts a schema in ways
// that are hard to detect and harder to unwind, and it fails rarely enough
// that nobody notices until production. It matters most in exactly the
// deployment shape dbtools targets with its private-network job story: a
// migration job that can be triggered twice, or retried while the first
// execution is still running.
//
// Scope is the whole apply run, not one statement or one transaction:
// migrations are multi-statement, span multiple files, and on some engines
// cannot run inside a transaction at all. That rules out relying on
// transactional locking, and is why each engine's session-scoped advisory
// lock is used instead.
package dblock

import (
	"context"
	"database/sql"
	"fmt"
	"hash/fnv"
)

// Lock is a held advisory lock. Release must be called to hand it back;
// holding it open for the life of a process would block every later run.
type Lock struct {
	release func(context.Context) error
	// conn is the single connection the lock lives on. Advisory locks are
	// session-scoped on every engine dbtools supports, so the lock and its
	// release have to run on the same connection — a pooled *sql.DB would
	// happily release on a different one, silently doing nothing.
	conn *sql.Conn
}

// Release hands the lock back and returns the connection to the pool.
// Safe to call more than once; only the first call does anything.
func (l *Lock) Release(ctx context.Context) error {
	if l == nil || l.conn == nil {
		return nil
	}
	relErr := l.release(ctx)
	closeErr := l.conn.Close()
	l.conn = nil
	if relErr != nil {
		return relErr
	}
	return closeErr
}

// Acquirer takes an engine's advisory lock on conn. Implementations block
// until the lock is available or ctx is done — they never return "busy" as
// a success.
type Acquirer func(ctx context.Context, conn *sql.Conn, key string) error

// Releaser hands an engine's advisory lock back on conn.
type Releaser func(ctx context.Context, conn *sql.Conn, key string) error

// Acquire takes the advisory lock named key on db and returns a handle that
// must be released.
//
// It deliberately blocks rather than failing fast when another run holds the
// lock: the common case is a second job execution starting while the first
// is still applying, and waiting is what the operator wants. A caller that
// prefers to give up passes a ctx with a deadline.
func Acquire(ctx context.Context, db *sql.DB, key string, acquire Acquirer, release Releaser) (*Lock, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("opening a connection for the migration lock: %w", err)
	}
	if err := acquire(ctx, conn, key); err != nil {
		conn.Close()
		return nil, fmt.Errorf("acquiring the migration lock: %w", err)
	}
	return &Lock{
		conn: conn,
		release: func(ctx context.Context) error {
			if err := release(ctx, conn, key); err != nil {
				return fmt.Errorf("releasing the migration lock: %w", err)
			}
			return nil
		},
	}, nil
}

// KeyFor derives a stable lock key for a database.
//
// The key is scoped to the database being migrated rather than global, so
// two dbtools runs against different databases on the same server do not
// serialise against each other for no reason.
func KeyFor(database string) string {
	if database == "" {
		return "dbtools"
	}
	return "dbtools-" + database
}

// NumericKey folds a key into the 64-bit integer engines that only accept
// numeric lock identifiers (Postgres) require.
//
// A hash collision would make two different databases share a lock, which
// costs concurrency but is never incorrect — the opposite trade would be
// unsafe, so this is the right direction to fail.
func NumericKey(key string) int64 {
	h := fnv.New64a()
	// Write never returns an error for a hash.
	_, _ = h.Write([]byte(key))
	return int64(h.Sum64())
}
