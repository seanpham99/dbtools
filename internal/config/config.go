package config

import (
	"errors"
	"fmt"
	"os"
	"sort"

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
