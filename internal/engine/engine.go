// Package engine defines the seam where each database engine plugs into
// dbtools. An Engine bundles everything that differs between databases:
// raw connection opening, DDL object-pattern parsing and existence checks
// for drift detection, migration-ledger SQL, and live-schema introspection
// for `generate`.
//
// golang-migrate driver registration is the one engine-specific concern
// not expressed on the interface: golang-migrate routes by URL scheme
// itself, so each engine package registers its migrate driver under its
// scheme in init() (see internal/migrator/godriver.go for the MSSQL one).
//
// Engines self-register via Register (normally from their package's
// init()), keyed by Name(), which doubles as the target-URL scheme —
// resolving an engine from a connection string is a scheme lookup.
package engine

import (
	"database/sql"
	"fmt"
	"net/url"
	"sort"

	"github.com/dbtools/dbtools/internal/ddlcheck"
	"github.com/dbtools/dbtools/internal/generate"
	"github.com/dbtools/dbtools/internal/ledger"
	"github.com/dbtools/dbtools/internal/migrator"
)

// Engine is one pluggable database engine implementation.
type Engine interface {
	// Name is both the engine's identifier in dbtools.toml and its
	// target-URL scheme (e.g. "mssql" for mssql:// connection strings).
	Name() string

	// Open opens a raw database/sql connection to a target URL, for
	// callers that need direct SQL access alongside golang-migrate's own
	// tracked connection — the migration ledger and the drift detector.
	Open(rawURL string) (*sql.DB, error)

	// DDL is the engine's dialect for parsing migration DDL and checking
	// whether the objects it names exist in a live database.
	DDL() DDLDialect

	// Ledger is the engine's migration-ledger store (the
	// dbtools_migration_history table's DDL/DML in this dialect).
	Ledger() LedgerStore

	// Introspect reads the live schema and returns one TableSchema per
	// base table (plus any engine-specific extras), for `generate`. The
	// second return value lists columns whose type had no Python mapping.
	Introspect(db *sql.DB, excludeList []string) ([]generate.TableSchema, []string, error)
}

// DDLDialect parses migration DDL for the named objects it creates or
// drops, and checks their existence — the primitives verify and repair
// reason with.
type DDLDialect interface {
	ExtractObjects(sqlText string) []ddlcheck.ObjectRef
	ExtractDroppedObjects(sqlText string) []ddlcheck.ObjectRef
	Exists(db *sql.DB, ref ddlcheck.ObjectRef) (bool, error)
}

// LedgerStore is the engine-dialect implementation of the migration
// ledger (see internal/ledger for the semantics each method must keep).
type LedgerStore interface {
	// Sync ensures the ledger table exists and backfills a row for every
	// version the migrate cursor already considers applied. Refuses to
	// backfill when the cursor is dirty.
	Sync(db *sql.DB, m *migrator.Migrator, migrationsDir string) error
	// SetStatus upserts version's ledger row, preserving content_sha256
	// when the row already exists.
	SetStatus(db ledger.DBTX, version uint64, status ledger.Status, note string) error
	// SetStatusWithHash is SetStatus plus recording the applied migration
	// file's content hash, so verify can detect edits after apply. Used by
	// the apply path only.
	SetStatusWithHash(db ledger.DBTX, version uint64, status ledger.Status, note, contentHash string) error
	List(db ledger.DBTX) ([]ledger.Entry, error)
	AppliedVersions(db ledger.DBTX) ([]uint64, error)
}

var registry = map[string]Engine{}

// Register adds e to the engine registry, keyed by e.Name(). Call from an
// engine package's init(). Registering the same name twice panics — it is
// always a wiring bug.
func Register(e Engine) {
	name := e.Name()
	if _, dup := registry[name]; dup {
		panic(fmt.Sprintf("engine %q registered twice", name))
	}
	registry[name] = e
}

// Names returns every registered engine name, sorted.
func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ForName returns the registered engine called name.
func ForName(name string) (Engine, error) {
	e, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown engine %q (available: %v)", name, Names())
	}
	return e, nil
}

// ForURL resolves the engine for rawURL by its scheme.
func ForURL(rawURL string) (Engine, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parsing connection URL: %w", err)
	}
	if u.Scheme == "" {
		return nil, fmt.Errorf("connection URL has no scheme (want one of %v, e.g. mssql://...)", Names())
	}
	return ForName(u.Scheme)
}

// ForTarget resolves the engine for a target, cross-checking the
// optional engine name configured in dbtools.toml against the connection
// URL's scheme. An empty engineName means "infer from the URL scheme".
func ForTarget(engineName, rawURL string) (Engine, error) {
	fromURL, err := ForURL(rawURL)
	if err != nil {
		return nil, err
	}
	if engineName != "" && engineName != fromURL.Name() {
		return nil, fmt.Errorf(
			"target's configured engine %q does not match its connection URL scheme %q — fix dbtools.toml or the URL",
			engineName, fromURL.Name())
	}
	return fromURL, nil
}
