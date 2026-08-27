package config

import (
	"errors"
	"fmt"
	"os"
	"sort"

	toml "github.com/pelletier/go-toml/v2"
	"github.com/seanpham99/dbtools/internal/dburl"
	"github.com/seanpham99/dbtools/internal/ledger"
)

// UnsetEnvError indicates a target's connection URL environment variable is not defined.
type UnsetEnvError struct {
	Target string
	URLEnv string
}

func (e *UnsetEnvError) Error() string {
	return fmt.Sprintf("target %q: environment variable %s is not set", e.Target, e.URLEnv)
}

// IsUnsetEnv reports whether err is an UnsetEnvError.
func IsUnsetEnv(err error) bool {
	var unset *UnsetEnvError
	return errors.As(err, &unset)
}

// Target is one named database environment declared in dbtools.toml.
// Its connection string is never stored in the file — URLEnv names the
// environment variable that holds it at runtime.
type Target struct {
	URLEnv string `toml:"url_env"`
	// Engine optionally names the target's database engine ("mssql").
	// Empty means "infer from the connection URL's scheme"; when set, it
	// must match that scheme (validated at engine resolution, see
	// internal/engine.ForTarget).
	Engine string `toml:"engine"`
	// Protected marks a target as not-for-routine-writes: push/repair/
	// reset refuse it unless the flag is removed. Verify/status (read-only)
	// still work. The config author's explicit "this is prod, be careful".
	Protected bool `toml:"protected"`
}

// GenerateConfig holds settings for pydantic model generation.
type GenerateConfig struct {
	Exclude []string `toml:"exclude"`
	Out     string   `toml:"out"`
}

// CloneConfig holds settings for `dbtools clone`.
type CloneConfig struct {
	// Exclude lists additional tables never to clone, unioned with
	// Generate.Exclude (which already protects dbtools's own bookkeeping
	// tables — see Load's defaulting below).
	Exclude []string `toml:"exclude"`
	// Mask maps a column name (case-insensitive) to a masking strategy:
	// "redact", "email", or "hash". Columns not listed here but matching
	// a small built-in sensitive-name list (email, phone, ssn, password)
	// are masked with a default strategy unless --no-mask is passed —
	// see internal/clone's maskPlanFor.
	Mask map[string]string `toml:"mask"`
}

// ProjectConfig identifies this project for scoping tool-owned local
// containers/volumes so two dbtools projects on one machine never collide.
type ProjectConfig struct {
	// Name overrides the default path-hash-derived container/volume
	// naming with a human-readable one (see internal/projectid.Resolve).
	Name string `toml:"name"`
}

// ContainerConfig configures the tool-owned local database container.
type ContainerConfig struct {
	// Port pins the host port `dbtools start` publishes the container on.
	// 0 (the default) means: let Docker assign a free port.
	Port int `toml:"port"`
}

// LedgerConfig configures the migration-ledger table dbtools reads and writes.
type LedgerConfig struct {
	// Table overrides the ledger table name — set this to coexist with an
	// incumbent migration tool's table (e.g. "schema_migrations") instead
	// of dbtools' own "dbtools_migration_history".
	Table string `toml:"table"`

	// CursorTable overrides the *version cursor* table — golang-migrate's
	// single-row (version, dirty) table, which is a different thing from
	// Table above and defaults to golang-migrate's own "schema_migrations".
	//
	// Set this when an incumbent tool already owns a table of that name.
	// "schema_migrations" is what golang-migrate, Rails, Supabase and many
	// hand-rolled runners all call their ledger, and those tables are not
	// shaped like golang-migrate's — a text version column and no "dirty"
	// column is the common case. Pointing the cursor elsewhere
	// (cursor_table = "dbtools_schema_version") lets dbtools coexist with
	// the incumbent rather than colliding with it.
	CursorTable string `toml:"cursor_table"`
}

// MigrationsConfig configures how migration files on disk are recognized.
type MigrationsConfig struct {
	// UpSuffix overrides the up-migration filename suffix (default
	// ".up.sql"). Set to ".sql" for a flat "<version>_<name>.sql" layout.
	UpSuffix string `toml:"up_suffix"`
}

// Defaults for the three migration-location values below, shared with
// ResolveDefaults so every caller (Load included) derives them once.
const (
	DefaultMigrationsDir = "migrations"
	DefaultUpSuffix      = ".up.sql"
	DefaultLedgerTable   = "dbtools_migration_history"

	// DefaultCursorTable is golang-migrate's own default version-cursor
	// table name. It is deliberately left as-is rather than namespaced:
	// every database dbtools has already migrated keeps its cursor here,
	// and silently moving it would make dbtools believe nothing had been
	// applied and try to replay the entire history. Users who collide with
	// an incumbent table of the same name set [ledger] cursor_table.
	DefaultCursorTable = "schema_migrations"

	// FallbackCursorTable is where the cursor moves when the ledger has
	// been pointed at DefaultCursorTable, so the two never share a name.
	// It is also the value the collision diagnostic suggests.
	FallbackCursorTable = "dbtools_schema_version"
)

// ResolveDefaults fills in the standard default for any of migrationsDir,
// upSuffix, or table that is empty. Load applies these to a *Config
// directly; this exists for the internal packages (repair, verify, down,
// apply, rollback, adopt) that take the three as plain parameters rather
// than a *Config, so each one defaults identically instead of every
// caller re-deriving its own copy of the same three checks.
func ResolveDefaults(migrationsDir, upSuffix, table string) (resolvedDir, resolvedSuffix, resolvedTable string) {
	if migrationsDir == "" {
		migrationsDir = DefaultMigrationsDir
	}
	if upSuffix == "" {
		upSuffix = DefaultUpSuffix
	}
	if table == "" {
		table = DefaultLedgerTable
	}
	return migrationsDir, upSuffix, table
}

// Config is the parsed contents of dbtools.toml.
type Config struct {
	MigrationsDir string            `toml:"migrations_dir"`
	Targets       map[string]Target `toml:"targets"`
	Generate      GenerateConfig    `toml:"generate"`
	Clone         CloneConfig       `toml:"clone"`
	Project       ProjectConfig     `toml:"project"`
	Container     ContainerConfig   `toml:"container"`
	Ledger        LedgerConfig      `toml:"ledger"`
	Migrations    MigrationsConfig  `toml:"migrations"`
}

// Load reads and parses the dbtools.toml file at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	cfg := &Config{}
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	cfg.MigrationsDir, cfg.Migrations.UpSuffix, cfg.Ledger.Table = ResolveDefaults(cfg.MigrationsDir, cfg.Migrations.UpSuffix, cfg.Ledger.Table)
	if err := ledger.ValidateTableName(cfg.Ledger.Table); err != nil {
		return nil, fmt.Errorf("dbtools.toml: %w", err)
	}
	if cfg.Ledger.CursorTable == "" {
		// An unset cursor_table normally keeps golang-migrate's default.
		// The exception is a config that points the *ledger* at that same
		// name — the documented way to coexist with an incumbent tool. Two
		// differently-shaped tables cannot share one name, so move the
		// cursor aside rather than making the user discover the conflict
		// through a driver error about a missing "dirty" column.
		if cfg.Ledger.Table == DefaultCursorTable {
			cfg.Ledger.CursorTable = FallbackCursorTable
		} else {
			cfg.Ledger.CursorTable = DefaultCursorTable
		}
	}
	if err := ledger.ValidateTableName(cfg.Ledger.CursorTable); err != nil {
		return nil, fmt.Errorf("dbtools.toml: cursor_table: %w", err)
	}
	if cfg.Ledger.CursorTable == cfg.Ledger.Table {
		return nil, fmt.Errorf("dbtools.toml: [ledger] table and cursor_table are both %q — "+
			"they are two different tables with incompatible shapes and cannot share a name", cfg.Ledger.Table)
	}
	// Default exclude list to internal tool tables unless the key was set at all
	// (including explicitly to an empty list, which means "exclude nothing").
	if cfg.Generate.Exclude == nil {
		exclude := []string{"dbtools_migration_history", "schema_migrations"}
		if cfg.Ledger.Table != DefaultLedgerTable {
			exclude = append(exclude, cfg.Ledger.Table)
		}
		if cfg.Ledger.CursorTable != DefaultCursorTable {
			exclude = append(exclude, cfg.Ledger.CursorTable)
		}
		cfg.Generate.Exclude = exclude
	}
	return cfg, nil
}

// CursorTableName returns the configured version-cursor table, defaulting
// for a Config that was built in memory rather than through Load.
func (c *Config) CursorTableName() string {
	if c.Ledger.CursorTable == "" {
		return DefaultCursorTable
	}
	return c.Ledger.CursorTable
}

// ResolveURL returns the connection string for targetName by reading the
// environment variable it names. It never reads a literal URL from the
// config file itself.
func (c *Config) ResolveURL(targetName string) (string, error) {
	t, ok := c.Targets[targetName]
	if !ok {
		return "", fmt.Errorf("unknown target %q (known targets: %v)", targetName, c.TargetNames())
	}
	url := os.Getenv(t.URLEnv)
	if url == "" {
		return "", &UnsetEnvError{Target: targetName, URLEnv: t.URLEnv}
	}
	return c.withCursorTable(url), nil
}

// ResolveURLOrFlag returns the connection string for targetName, preferring
// an explicit --url flag value over the configured environment variable.
// Used by commands that need to reach a database whose URL is only known
// at runtime (e.g. deploy scripts that source .env.prod into env vars and
// pass them straight through as flags).
func (c *Config) ResolveURLOrFlag(targetName, urlFlag string) (string, error) {
	if urlFlag != "" {
		return c.withCursorTable(urlFlag), nil
	}
	return c.ResolveURL(targetName)
}

// withCursorTable annotates rawURL with golang-migrate's
// x-migrations-table parameter when a non-default cursor table is
// configured. golang-migrate reads it to decide which table holds the
// version cursor; every other consumer of the URL must strip it first
// (see dburl.StripCustomParams), because it is not a real connection
// parameter and drivers reject it.
//
// Carrying it on the URL rather than threading a parameter through the
// dozen call sites of migrator.Open keeps one source of truth for "which
// cursor table", at the cost of that stripping discipline.
func (c *Config) withCursorTable(rawURL string) string {
	cursor := c.CursorTableName()
	if cursor == DefaultCursorTable || rawURL == "" {
		return rawURL
	}
	return dburl.WithParam(rawURL, "x-migrations-table", cursor)
}

// EngineName returns the engine configured for targetName, or "" when
// the target is unknown or has no explicit engine (meaning: infer from
// the connection URL's scheme).
func (c *Config) EngineName(targetName string) string {
	return c.Targets[targetName].Engine
}

// TargetNames returns all configured target names in sorted order.
func (c *Config) TargetNames() []string {
	names := make([]string, 0, len(c.Targets))
	for name := range c.Targets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
