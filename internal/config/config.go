package config

import (
	"fmt"
	"os"
	"sort"

	toml "github.com/pelletier/go-toml/v2"
)

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
}

// GenerateConfig holds settings for pydantic model generation.
type GenerateConfig struct {
	Exclude []string `toml:"exclude"`
	Out     string   `toml:"out"`
}

// Config is the parsed contents of dbtools.toml.
type Config struct {
	MigrationsDir string            `toml:"migrations_dir"`
	Targets       map[string]Target `toml:"targets"`
	Generate      GenerateConfig    `toml:"generate"`
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
	if cfg.MigrationsDir == "" {
		cfg.MigrationsDir = "migrations"
	}
	// Default exclude list to internal tool tables unless the key was set at all
	// (including explicitly to an empty list, which means "exclude nothing").
	if cfg.Generate.Exclude == nil {
		cfg.Generate.Exclude = []string{"dbtools_migration_history", "schema_migrations"}
	}
	return cfg, nil
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
		return "", fmt.Errorf("target %q: environment variable %s is not set", targetName, t.URLEnv)
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
