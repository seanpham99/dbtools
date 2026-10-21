package localdb

import (
	"time"

	"github.com/seanpham99/dbtools/internal/container"
)

// dockerProvider hosts the local database in a container. It is the
// default because it works for every engine, and because a container
// isolates one project's database from every other project on the
// machine.
type dockerProvider struct {
	engineName string
}

func (p dockerProvider) Name() string { return ProviderDocker }

func (p dockerProvider) Start(projectID string, port string, timeout time.Duration, wait bool) (string, error) {
	return container.StartForWithTimeout(p.engineName, projectID, port, timeout, wait)
}

func (p dockerProvider) Stop(projectID string, purge bool) error {
	return container.StopFor(p.engineName, projectID, purge)
}

func (p dockerProvider) Restart(projectID string, port string, timeout time.Duration, wait bool) (string, error) {
	return container.RestartForWithTimeout(p.engineName, projectID, port, timeout, wait)
}

func (p dockerProvider) Logs(projectID string, follow bool) error {
	return container.LogsFor(p.engineName, projectID, follow)
}

func (p dockerProvider) MaintenanceURL(projectID string, localURL string) (string, error) {
	return container.MaintenanceURLFor(p.engineName, localURL)
}
