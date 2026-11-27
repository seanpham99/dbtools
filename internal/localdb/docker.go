package localdb

import (
	"fmt"
	"time"

	"github.com/seanpham99/dbtools/internal/container"
	"github.com/seanpham99/dbtools/internal/logger"
	"github.com/seanpham99/dbtools/internal/support"
)

// dockerProvider hosts the local database in a container. It is the
// default because it works for every engine, and because a container
// isolates one project's database from every other project on the
// machine.
type dockerProvider struct {
	engineName string
}

func (p dockerProvider) Name() string { return ProviderDocker }

// Major returns the Postgres major the local container runs for requested.
//
// The docker provider can serve any major it has an image tag for, so a
// pin is honored and the container's image follows it. When nothing is
// pinned it serves the image the local database has always used, which is
// also what an existing unpinned project's data directory belongs to.
func (p dockerProvider) Major(requested string) (string, error) {
	if p.engineName != "postgres" {
		if requested != "" {
			return "", fmt.Errorf("dbtools.toml: [local] postgres_major applies to the postgres engine only, but the local target uses %s", p.engineName)
		}
		return "", nil
	}
	if requested == "" {
		return container.DefaultPostgresMajor, nil
	}
	if err := validateMajor(requested); err != nil {
		return "", err
	}
	// A pin outside the tested window is honored: it can legitimately
	// mirror a legacy target, and refusing it would leave that project
	// unable to compare schemas at all. It is said out loud instead.
	if ok, msg := support.Check("postgres", requested); !ok {
		logger.Warnf("dbtools.toml: [local] postgres_major = %q: %s", requested, msg)
	}
	return requested, nil
}

func (p dockerProvider) Start(projectID, port, major string, timeout time.Duration, wait bool) (string, error) {
	return container.StartForWithTimeout(p.engineName, projectID, port, major, timeout, wait)
}

func (p dockerProvider) Stop(projectID string, purge bool) error {
	return container.StopFor(p.engineName, projectID, purge)
}

func (p dockerProvider) Restart(projectID, port, major string, timeout time.Duration, wait bool) (string, error) {
	return container.RestartForWithTimeout(p.engineName, projectID, port, major, timeout, wait)
}

func (p dockerProvider) Logs(projectID string, follow bool) error {
	return container.LogsFor(p.engineName, projectID, follow)
}

func (p dockerProvider) MaintenanceURL(projectID string, localURL string) (string, error) {
	return container.MaintenanceURLFor(p.engineName, localURL)
}
