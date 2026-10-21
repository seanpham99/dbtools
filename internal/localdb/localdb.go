// Package localdb hosts the tool-owned local database — the database that
// `dbtools start` provisions for a project and that `up` migrates.
//
// Two providers exist. The docker provider runs a container, which is the
// default and the only one that works for every engine. The pgx provider
// runs an external pgx runtime, which speaks the PostgreSQL wire protocol
// and therefore serves the postgres engine only.
//
// The provider is chosen by the [local] provider setting in dbtools.toml.
// A missing or unreadable config leaves the docker provider in place,
// because stop and logs have to keep working in a directory that has no
// dbtools.toml.
package localdb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/container"
	"github.com/seanpham99/dbtools/internal/pgx"
)

// Provider names of the supported providers.
const (
	// ProviderDocker runs the local database in a container.
	ProviderDocker = "docker"
	// ProviderPgX runs the local database in a pgx runtime.
	ProviderPgX = "pgx"
)

// DatabaseName is the name of the tool-owned local database. Both
// providers use it, so a project can switch providers without renaming
// anything.
const DatabaseName = container.DatabaseName

// Provider hosts the tool-owned local database of one project.
type Provider interface {
	// Name identifies the provider in messages.
	Name() string
	// Start provisions the local database and returns its connection URL.
	Start(projectID string, port string, timeout time.Duration, wait bool) (string, error)
	// Stop shuts the local database down. purge also deletes its data.
	Stop(projectID string, purge bool) error
	// Restart restarts the local database, keeping its data.
	Restart(projectID string, port string, timeout time.Duration, wait bool) (string, error)
	// Logs prints the local database's server log.
	Logs(projectID string, follow bool) error
	// MaintenanceURL returns the URL of the server's maintenance database,
	// which reset drops and recreates the local database through.
	MaintenanceURL(projectID string, localURL string) (string, error)
}

// Select returns the provider that hosts the tool-owned local database.
// The engine name decides which providers can serve the target.
func Select(cfg *config.Config, engineName string) (Provider, error) {
	name := ProviderName(cfg)
	switch name {
	case ProviderDocker:
		return dockerProvider{engineName: engineName}, nil
	case ProviderPgX:
		if engineName != "postgres" {
			return nil, fmt.Errorf("the %s local provider serves the postgres engine only, but the local target uses %s", ProviderPgX, engineName)
		}
		if !pgx.Available() {
			return nil, pgx.MissingBinaryError()
		}
		return pgxProvider{engineName: engineName}, nil
	default:
		return nil, fmt.Errorf("unknown local provider %q (supported: %s, %s)", name, ProviderDocker, ProviderPgX)
	}
}

// ProviderName returns the configured provider name, or the docker
// default when none is set.
func ProviderName(cfg *config.Config) string {
	if cfg == nil {
		return ProviderDocker
	}
	name := strings.ToLower(strings.TrimSpace(cfg.Local.Provider))
	if name == "" {
		return ProviderDocker
	}
	return name
}

// Start provisions the tool-owned local database of one project and
// returns its connection URL.
func Start(cfg *config.Config, engineName, projectID, port string, timeout time.Duration, wait bool) (string, error) {
	p, err := Select(cfg, engineName)
	if err != nil {
		return "", err
	}
	return p.Start(projectID, port, timeout, wait)
}

// Stop shuts the tool-owned local database down. purge also deletes its
// data.
func Stop(cfg *config.Config, engineName, projectID string, purge bool) error {
	p, err := Select(cfg, engineName)
	if err != nil {
		return err
	}
	return p.Stop(projectID, purge)
}

// Restart restarts the tool-owned local database, keeping its data, and
// returns its connection URL.
func Restart(cfg *config.Config, engineName, projectID, port string, timeout time.Duration, wait bool) (string, error) {
	p, err := Select(cfg, engineName)
	if err != nil {
		return "", err
	}
	return p.Restart(projectID, port, timeout, wait)
}

// Logs prints the tool-owned local database's server log.
func Logs(cfg *config.Config, engineName, projectID string, follow bool) error {
	p, err := Select(cfg, engineName)
	if err != nil {
		return err
	}
	return p.Logs(projectID, follow)
}

// MaintenanceURL returns the URL of the maintenance database that reset
// drops and recreates the local database through.
//
// The provider owns the answer. The docker provider re-derives it from
// the container; the pgx provider re-derives it from the cluster. Neither
// takes a maintenance URL out of user configuration, so a mistyped target
// URL can never aim the drop at a real server.
func MaintenanceURL(cfg *config.Config, engineName, projectID, localURL string) (string, error) {
	p, err := Select(cfg, engineName)
	if err != nil {
		return "", err
	}
	return p.MaintenanceURL(projectID, localURL)
}

// PruneReport lists what Prune removed or would remove.
type PruneReport struct {
	// Clusters are the pgx cluster directories that were removed, or
	// would be removed on a dry run.
	Clusters []string
	// StoppedClusters are the pgx clusters whose runtime was stopped
	// because their owning project directory is gone. Their data
	// survives, the way a pruned container's volume does.
	StoppedClusters []string
	// Skipped is set when the pgx half of the cleanup could not run at
	// all, because the pgx binary is not installed. Reason explains it.
	Skipped bool
	// Reason explains why the pgx half was skipped.
	Reason string
}

// Prune cleans up the pgx clusters whose owning project directory is
// gone — the leak left behind when a git worktree is deleted, where
// nothing runs dbtools stop.
//
// A pgx cluster holds the runtime and the data together, so it cannot be
// split the way a container and its volume can. Without removeData the
// runtime is stopped and the data is kept, which matches what the docker
// provider does to a container it prunes. With removeData the whole
// cluster directory goes, which destroys that project's local database.
//
// dryRun reports without changing anything.
func Prune(removeData bool, dryRun bool) (PruneReport, error) {
	var rep PruneReport
	if !pgx.Available() {
		// Without the pgx binary no runtime can be stopped, and a
		// running runtime's data directory must not be deleted from
		// under it. The cleanup is skipped rather than failed: a host
		// that never used pgx has nothing to clean up.
		rep.Skipped = true
		rep.Reason = pgx.InstallHint
		return rep, nil
	}
	root, err := pgx.Root()
	if err != nil {
		return rep, err
	}
	engineDirs, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return rep, nil
		}
		return rep, fmt.Errorf("reading %s: %w", root, err)
	}
	for _, engineDir := range engineDirs {
		if !engineDir.IsDir() || engineDir.Name() == "run" {
			continue
		}
		engineRoot := filepath.Join(root, engineDir.Name())
		projectDirs, err := os.ReadDir(engineRoot)
		if err != nil {
			return rep, fmt.Errorf("reading %s: %w", engineRoot, err)
		}
		for _, projectDir := range projectDirs {
			if !projectDir.IsDir() {
				continue
			}
			home := filepath.Join(engineRoot, projectDir.Name())
			owner := pgx.Owner(home)
			if owner == "" {
				continue
			}
			if _, err := os.Stat(owner); err == nil {
				continue
			}
			client := &pgx.Client{
				Home:   home,
				RunDir: filepath.Join(root, "run", engineDir.Name(), projectDir.Name()),
			}
			if !removeData {
				// A cluster whose runtime a previous prune already
				// stopped is not reported again: the leak is closed,
				// and reporting it every time would be noise. Its
				// data stays until a prune --volumes removes it.
				if pgx.IsPruned(home) {
					continue
				}
				// A dry run stops nothing and marks nothing.
				if !dryRun {
					if err := client.Stop(context.Background()); err != nil {
						return rep, err
					}
					if err := pgx.MarkPruned(home); err != nil {
						return rep, err
					}
				}
				rep.StoppedClusters = append(rep.StoppedClusters, home)
				continue
			}
			if err := client.Stop(context.Background()); err != nil {
				return rep, err
			}
			rep.Clusters = append(rep.Clusters, home)
			if dryRun {
				continue
			}
			if err := os.RemoveAll(home); err != nil {
				return rep, fmt.Errorf("removing %s: %w", home, err)
			}
		}
	}
	return rep, nil
}
