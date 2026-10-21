// cmd/localdb.go
package cmd

import (
	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/localdb"
)

// The variables below are the injection seams the local-database
// lifecycle commands use. A test replaces one to observe the call without
// starting a real database.
var (
	startLocalDB   = localdb.Start
	stopLocalDB    = localdb.Stop
	restartLocalDB = localdb.Restart
	logsLocalDB    = localdb.Logs
	// pruneLocalDB removes the pgx clusters whose owning project
	// directory is gone.
	pruneLocalDB = localdb.Prune
)

// localMaintenanceURL returns the URL of the maintenance database that
// reset drops and recreates the local database through. The provider owns
// the answer, so the URL never comes out of user configuration.
//
// The engine and the project identity come from the local target: reset
// is scoped to the tool-owned local database, never to a remote server.
var localMaintenanceURL = func(engineName string, localURL string) (string, error) {
	cfg := localConfigOrNil()
	projectID := loadProjectIDOrDefault()
	return localdb.MaintenanceURL(cfg, engineName, projectID, localURL)
}

// localConfigOrNil returns the parsed dbtools.toml, or nil when there is
// none.
//
// stop and logs have to keep working in a directory that has no
// dbtools.toml, so the local lifecycle cannot require one. A nil config
// selects the default provider.
func localConfigOrNil() *config.Config {
	cfg, err := loadConfig("dbtools.toml")
	if err != nil {
		return nil
	}
	return cfg
}
