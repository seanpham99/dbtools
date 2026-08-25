package cmd

import (
	"strconv"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/migrator"
	"github.com/seanpham99/dbtools/internal/projectid"
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
		if scheme := migrator.SchemeOf(rawURL); scheme != "" {
			return scheme
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

// loadProjectID resolves the current project's container/volume scoping
// identity from an already-loaded cfg (see internal/projectid.Resolve):
// cfg.Project.Name overrides the default path-hash-derived identity.
func loadProjectID(cfg *config.Config) (string, error) {
	return projectid.Resolve("dbtools.toml", cfg.Project.Name)
}

// loadProjectIDOrDefault loads dbtools.toml itself and resolves the
// project identity, falling back to a path-hash even when the config
// can't be loaded at all — mirrors loadLocalEngineName's leniency so
// `dbtools stop` keeps working in a directory with no config.
func loadProjectIDOrDefault() string {
	cfg, err := loadConfig("dbtools.toml")
	name := ""
	if err == nil {
		name = cfg.Project.Name
	}
	id, err := projectid.Resolve("dbtools.toml", name)
	if err != nil {
		id, _ = projectid.Resolve("dbtools.toml", "")
	}
	return id
}

// configuredContainerPort returns the pinned host port from dbtools.toml's
// [container] port, or "" when unset — meaning "let Docker assign a free
// port".
func configuredContainerPort(cfg *config.Config) string {
	if cfg.Container.Port == 0 {
		return ""
	}
	return strconv.Itoa(cfg.Container.Port)
}
