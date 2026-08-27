package migrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/seanpham99/dbtools/internal/dblock"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/ledger"
)

// Runner applies and reverts migrations against one target.
//
// It replaces golang-migrate. The reason is not that golang-migrate applies
// files badly — it is that owning the loop is what lets the ledger be the
// single source of truth (no separate version cursor to disagree with it),
// what lets migration filenames follow the project's convention rather than
// the library's, and what removes the cursor-table collision that made
// dbtools unusable against a database an incumbent tool already managed.
// See docs/adr/003-v0.7-native-runner.md.
//
// Every write path takes the migration lock for the whole run and refuses to
// start when a previous run left a migration mid-apply.
type Runner struct {
	eng         engine.Engine
	db          *sql.DB
	dir         *Dir
	ledgerTable string
}

// NewRunner builds a Runner for an already-open database.
//
// db is not closed by the Runner; the caller owns it, because callers
// generally need the same connection for the ledger and drift checks.
func NewRunner(eng engine.Engine, db *sql.DB, dir *Dir, ledgerTable string) *Runner {
	return &Runner{
		eng:         eng,
		db:          db,
		dir:         dir,
		ledgerTable: ledgerTable,
	}
}

// State reports the current version and whether a previous run left a
// migration mid-apply.
//
// Read-only: it takes no lock and creates nothing. A database with no
// ledger table has had nothing applied to it, which is a state to report
// rather than an error — a status query must never be the thing that
// writes to a database for the first time.
func (r *Runner) State(ctx context.Context) (ledger.State, error) {
	exists, err := engine.TableExists(r.eng, r.db, r.ledgerTable)
	if err != nil {
		return ledger.State{}, err
	}
	if !exists {
		return ledger.State{}, nil
	}
	return r.eng.Ledger().State(r.db, r.ledgerTable)
}

// Up applies every pending migration in ascending order and reports how
// many ran.
func (r *Runner) Up(ctx context.Context) (applied int, err error) {
	return r.apply(ctx, 0)
}

// Step applies at most limit pending migrations (limit <= 0 means all).
func (r *Runner) Step(ctx context.Context, limit int) (applied int, err error) {
	return r.apply(ctx, limit)
}

func (r *Runner) apply(ctx context.Context, limit int) (applied int, err error) {
	release, err := r.lock(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { err = errors.Join(err, release()) }()

	if err := r.eng.Ledger().EnsureSchema(r.db, r.ledgerTable); err != nil {
		return 0, err
	}
	state, err := r.eng.Ledger().State(r.db, r.ledgerTable)
	if err != nil {
		return 0, err
	}
	if state.Dirty {
		return 0, &ledger.DirtyError{Version: state.Applying, Table: r.ledgerTable}
	}

	pending := r.dir.PendingAfter(state.Version, state.HasVersion)
	if limit > 0 && len(pending) > limit {
		pending = pending[:limit]
	}
	if len(pending) == 0 {
		return 0, nil
	}

	conn, err := r.db.Conn(ctx)
	if err != nil {
		return 0, fmt.Errorf("opening a connection to apply migrations: %w", err)
	}
	defer conn.Close()

	for _, f := range pending {
		if err := r.applyOne(ctx, conn, f); err != nil {
			// Stop at the first failure with the count that did succeed:
			// those versions are recorded, and the caller needs to know the
			// run was partial rather than assume none of it happened.
			return applied, err
		}
		applied++
	}
	return applied, nil
}

// applyOne records the migration as applying, runs it, then records the
// outcome.
//
// The applying row is written *before* the SQL runs and is what makes a
// half-applied migration visible afterwards. If the process dies mid-file,
// that row survives and the next run refuses to continue — which is the
// whole point, because applying more SQL on top of a schema in an unknown
// state turns a recoverable failure into an unrecoverable one.
func (r *Runner) applyOne(ctx context.Context, conn *sql.Conn, f File) error {
	sqlText, err := f.Read()
	if err != nil {
		return err
	}
	hash, err := r.dir.ContentHash(f.Version)
	if err != nil {
		return err
	}
	store := r.eng.Ledger()

	if err := store.SetStatus(r.db, f.Version, ledger.StatusApplying, "apply started", r.ledgerTable); err != nil {
		return err
	}
	if err := r.eng.ExecMigration(ctx, conn, sqlText); err != nil {
		// Deliberately leave the applying row in place. Clearing it here
		// would erase the only durable evidence that this version touched
		// the schema, and on engines without transactional DDL some of its
		// statements may well have taken effect.
		return fmt.Errorf("migration %s: %w", f.Filename, err)
	}
	return store.SetStatusWithHash(r.db, f.Version, ledger.StatusApplied, "", hash, r.ledgerTable)
}

// Down reverts the last steps applied migrations using their .down.sql
// files, most recent first, and returns the versions it reverted.
//
// The ledger row for each version is written before the lock is released,
// so a caller cannot observe — or overwrite — a half-recorded revert.
func (r *Runner) Down(ctx context.Context, steps int) (revertedVersions []uint64, err error) {
	release, err := r.lock(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, release()) }()

	store := r.eng.Ledger()
	if err := store.EnsureSchema(r.db, r.ledgerTable); err != nil {
		return nil, err
	}
	state, err := store.State(r.db, r.ledgerTable)
	if err != nil {
		return nil, err
	}
	if state.Dirty {
		return nil, &ledger.DirtyError{Version: state.Applying, Table: r.ledgerTable}
	}

	appliedVersions, err := store.AppliedVersions(r.db, r.ledgerTable)
	if err != nil {
		return nil, err
	}
	plan, err := r.dir.DownPlan(appliedVersions, steps)
	if err != nil {
		return nil, err
	}
	if len(plan) == 0 {
		return nil, nil
	}

	conn, err := r.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("opening a connection to revert migrations: %w", err)
	}
	defer conn.Close()

	for _, f := range plan {
		sqlText, err := f.Read()
		if err != nil {
			return revertedVersions, err
		}
		hash, err := r.dir.DownContentHash(f.Version)
		if err != nil {
			return revertedVersions, err
		}
		if err := store.SetStatus(r.db, f.Version, ledger.StatusApplying, "revert started", r.ledgerTable); err != nil {
			return revertedVersions, err
		}
		if err := r.eng.ExecMigration(ctx, conn, sqlText); err != nil {
			return revertedVersions, fmt.Errorf("reverting migration %s: %w", f.Filename, err)
		}
		// Recorded here, inside the lock. Doing it after Down returns
		// would let a new run acquire the lock, mark this version
		// applying, and have that marker overwritten while its SQL is
		// still running.
		if err := store.SetStatusWithHash(r.db, f.Version, ledger.StatusReverted, "reverted via down", hash, r.ledgerTable); err != nil {
			return revertedVersions, err
		}
		revertedVersions = append(revertedVersions, f.Version)
	}
	return revertedVersions, nil
}

// Force records version as applied without running its SQL, clearing a
// stuck applying row.
//
// This is the "I have inspected the database myself and it really is in
// this state" escape hatch. It runs no migration SQL, so it can only ever
// be as correct as the operator's judgement — which is why it is a separate
// verb rather than something the apply path does on their behalf.
func (r *Runner) Force(ctx context.Context, version uint64) (err error) {
	release, err := r.lock(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, release()) }()

	store := r.eng.Ledger()
	if err := store.EnsureSchema(r.db, r.ledgerTable); err != nil {
		return err
	}
	return store.SetStatus(r.db, version, ledger.StatusApplied, "forced", r.ledgerTable)
}

// WithLock runs fn holding the migration lock for this target.
//
// Every command that writes the ledger needs it, not just the ones that run
// migration SQL: repair, rollback, adopt and squash all rewrite rows the
// runner relies on, and doing that while a migration is in flight can clear
// an "applying" marker for SQL that is still executing — which is exactly
// the state the marker exists to preserve.
func (r *Runner) WithLock(ctx context.Context, fn func() error) (err error) {
	release, err := r.lock(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, release()) }()
	return fn()
}

// LockForWrite takes the migration lock and returns a release function, for
// callers whose control flow does not fit WithLock's closure. The caller
// must defer the returned function.
func (r *Runner) LockForWrite(ctx context.Context) (func(), error) {
	release, err := r.lock(ctx)
	if err != nil {
		return nil, err
	}
	return func() { _ = release() }, nil
}

// lock takes the migration lock and returns its release function.
func (r *Runner) lock(ctx context.Context) (func() error, error) {
	acquire, release, err := dblock.ForEngine(r.eng.Name())
	if err != nil {
		return nil, err
	}
	key := dblock.KeyFor(dblock.DatabaseName(ctx, r.db, r.eng.Name()))
	held, err := dblock.Acquire(ctx, r.db, key, acquire, release)
	if err != nil {
		return nil, err
	}
	return func() error { return held.Release(ctx) }, nil
}
