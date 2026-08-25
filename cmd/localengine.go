package cmd

import (
	"net/url"
	"strings"

	"github.com/seanpham99/dbtools/internal/config"
)

// localEngineName determines which engine the "local" target uses, for
// container lifecycle commands that run before any connection exists:
// the target's explicit engine field wins; otherwise the URL scheme is
// inferred when the URL env var is already resolvable; otherwise the
// historical default, mssql.
func localEngineName(cfg *config.Config) string {
	if name := cfg.EngineName("local"); name != "" {
		return name
	}
	if rawURL, err := cfg.ResolveURL("local"); err == nil {
		if idx := strings.Index(rawURL, "://"); idx > 0 {
			return rawURL[:idx]
		}
		if u, err := url.Parse(rawURL); err == nil && u.Scheme != "" {
			return u.Scheme
		}
	}
	return "mssql"
}

// loadLocalEngineName loads dbtools.toml and returns the local target's
// engine name; when no config or local target exists it falls back to
// mssql so `dbtools stop` keeps working in a bare directory.
func loadLocalEngineName() string {
	cfg, err := loadConfig("dbtools.toml")
	if err != nil {
		return "mssql"
	}
	if _, ok := cfg.Targets["local"]; !ok {
		return "mssql"
	}
	return localEngineName(cfg)
}
