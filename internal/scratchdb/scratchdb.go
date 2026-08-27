package scratchdb

import (
	"fmt"
	"os"

	"github.com/seanpham99/dbtools/internal/container"
	"github.com/seanpham99/dbtools/internal/engine"
)

// Provision returns a connection URL to a throwaway database for eng,
// plus a cleanup function to tear it down. If against is non-empty, it's
// returned unchanged with a nil cleanup — the caller supplied their own
// pre-provisioned scratch database and owns its lifecycle. Otherwise:
// SQLite gets a tempfile (no server needed); every other engine gets an
// ephemeral, --rm Docker container via container.StartScratch.
func Provision(eng engine.Engine, against string) (url string, cleanup func() error, err error) {
	return ProvisionSeries(eng, against, "")
}

// ProvisionSeries is Provision pinned to a server version series, so the
// scratch database can be made to match the target it will be compared
// against. An empty series means "use the engine's default image", making it
// identical to Provision.
//
// Callers that compare schemas (diff, squash) should pass the target's major
// from ServerSeries: catalog rendering is not stable across versions, and a
// mismatched scratch database reports that difference as drift that does not
// exist. See container.ScratchImageFor.
func ProvisionSeries(eng engine.Engine, against, series string) (url string, cleanup func() error, err error) {
	if against != "" {
		return against, nil, nil
	}
	if eng.Name() == "sqlite" {
		f, err := os.CreateTemp("", "dbtools-scratch-*.db")
		if err != nil {
			return "", nil, fmt.Errorf("creating scratch file: %w", err)
		}
		path := f.Name()
		f.Close()
		os.Remove(path) // sqlite creates it fresh on first open
		return "sqlite://" + path, func() error { return os.Remove(path) }, nil
	}
	url, c, err := container.StartScratchSeries(eng.Name(), series)
	if err != nil {
		return "", nil, fmt.Errorf("provisioning scratch database: %w", err)
	}
	return url, c, nil
}
