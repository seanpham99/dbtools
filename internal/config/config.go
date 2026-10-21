package config

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
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
//
// Deprecated key: the port moved to [local] port. The key is still read so
// an existing dbtools.toml keeps working, but it names the setting after one
// provider rather than after what it configures — see LocalConfig.
//
// Deliberately not written as a "Deprecated:" marker: staticcheck would then
// flag the two places that legitimately construct this type — Config itself
// and the precedence logic that reads the legacy key.
type ContainerConfig struct {
	// Port pins the host port `dbtools start` publishes the container on.
	// 0 (the default) means: let Docker assign a free port.
	Port int `toml:"port"`
}

// PostgresMajor is the PostgreSQL major the local database runs, as
// written in dbtools.toml.
//
// It decodes from text so both spellings work — postgres_major = 17 and
// postgres_major = "17" — because the natural form for a version is a bare
// integer, and refusing that spelling would leave the setting unset and
// hand the project the silent provider default it exists to remove.
type PostgresMajor string

// UnmarshalText records the value verbatim. Whether it names a major the
// selected provider can serve is decided where the local database is
// provisioned, not where the configuration is parsed: config.Load runs on
// every command, and must not refuse `dbtools stop` over a bad pin.
func (m *PostgresMajor) UnmarshalText(text []byte) error {
	*m = PostgresMajor(strings.TrimSpace(string(text)))
	return nil
}

// LocalConfig configures the tool-owned local database.
//
// Every setting here describes the local database itself, not the provider
// that happens to host it. That is why the port lives here and not under
// [container]: the two providers take the same setting with different
// timing (see Config.LocalPort).
type LocalConfig struct {
	// Provider selects what hosts the tool-owned local database.
	// "docker" (the default) runs it in a container; "pgx" runs it in an
	// external pgx runtime, which serves the postgres engine only.
	Provider string `toml:"provider"`
	// Port pins the host port the local database listens on. nil means
	// unset — the provider assigns a free port. 0 means the same as unset
	// for the docker provider, and is distinct from nil so a project can
	// unpin a port the deprecated [container] port set.
	Port *int `toml:"port"`
	// PostgresMajor pins the Postgres major version the local database
	// runs. Empty means "the provider's default". It is a data-format
	// boundary, not a preference: an existing local database cannot move
	// between majors without discarding its data.
	PostgresMajor PostgresMajor `toml:"postgres_major"`
}

// LedgerConfig configures the migration-ledger table dbtools reads and writes.
type LedgerConfig struct {
	// Table overrides the ledger table name — set this to coexist with an
	// incumbent migration tool's table (e.g. "schema_migrations") instead
	// of dbtools' own "dbtools_migration_history".
	Table string `toml:"table"`
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
	Local         LocalConfig       `toml:"local"`
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
	// Default exclude list to internal tool tables unless the key was set at all
	// (including explicitly to an empty list, which means "exclude nothing").
	if cfg.Generate.Exclude == nil {
		exclude := []string{"dbtools_migration_history", "schema_migrations"}
		if cfg.Ledger.Table != DefaultLedgerTable {
			exclude = append(exclude, cfg.Ledger.Table)
		}
		cfg.Generate.Exclude = exclude
	}
	return cfg, nil
}

// LocalPort returns the host port the tool-owned local database is pinned
// to, and whether that value came from the deprecated [container] port.
//
// The canonical key is [local] port. [container] port is still honored so
// an existing dbtools.toml keeps working, but it is deprecated — it names
// the setting after the docker provider while it configures both. A zero
// port means "let the provider assign a free one": both providers treat an
// unpinned port that way, and [local] port = 0 is how a project unpins a
// value the deprecated key still carries.
//
// The providers apply the pin at different moments, which is the other
// half of why the setting is provider-neutral and its scope is documented
// here: the docker provider publishes the pinned port on every start, and
// the pgx provider records it once, when the cluster is initialised.
func (c *Config) LocalPort() (port int, deprecated bool) {
	if c == nil {
		return 0, false
	}
	if c.Local.Port != nil {
		return *c.Local.Port, false
	}
	return c.Container.Port, c.Container.Port != 0
}

// LedgerTableName returns the configured ledger table, defaulting for a
// Config built in memory rather than through Load.
//
// Every caller should use this rather than reading Ledger.Table directly:
// Load applies the default, but a Config assembled as a struct literal (in
// tests, and in any future programmatic use) would otherwise carry an empty
// table name straight into SQL.
func (c *Config) LedgerTableName() string {
	if c.Ledger.Table == "" {
		return DefaultLedgerTable
	}
	return c.Ledger.Table
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
	return url, nil
}

// ResolveURLOrFlag returns the connection string for targetName, preferring
// an explicit --url flag value over the configured environment variable.
// Used by commands that need to reach a database whose URL is only known
// at runtime (e.g. deploy scripts that source .env.prod into env vars and
// pass them straight through as flags).
func (c *Config) ResolveURLOrFlag(targetName, urlFlag string) (string, error) {
	if urlFlag != "" {
		return urlFlag, nil
	}
	return c.ResolveURL(targetName)
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
