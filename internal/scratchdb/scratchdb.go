package scratchdb

import (
	"fmt"
	"os"

	"github.com/seanpham99/dbtools/internal/container"
	"github.com/seanpham99/dbtools/internal/engine"
)

// Scratch is a throwaway database and whatever is needed to tear it down.
type Scratch struct {
	// URL connects to the scratch database from this process.
	URL string
	// Container names the Docker container hosting it, or "" when there
	// isn't one — a SQLite tempfile, or a database the caller supplied via
	// --against and owns themselves. Callers that need a version-matched
	// engine tool (see dump.Schema) run it inside this container.
	Container string
	// Cleanup tears the scratch database down. nil when the caller owns it.
	Cleanup func() error
}

// Provision returns a throwaway database for eng. If against is non-empty,
// it is returned unchanged with no container and no cleanup — the caller
// supplied their own and owns its lifecycle. Otherwise: SQLite gets a
// tempfile (no server needed); every other engine gets an ephemeral, --rm
// Docker container.
func Provision(eng engine.Engine, against string) (Scratch, error) {
	return ProvisionSeries(eng, against, "")
}

// ProvisionSeries is Provision pinned to a server version series, so the
// scratch database can be made to match the target it will be compared
// against. An empty series means "use the engine's default image", making it
// identical to Provision.
//
// Callers that compare schemas (diff, squash) should pass the target's series
// from ServerSeries: catalog rendering is not stable across versions, and a
// mismatched scratch database reports that difference as drift that does not
// exist. See container.ScratchImageFor.
func ProvisionSeries(eng engine.Engine, against, series string) (Scratch, error) {
	if against != "" {
		return Scratch{URL: against}, nil
	}
	if eng.Name() == "sqlite" {
		f, err := os.CreateTemp("", "dbtools-scratch-*.db")
		if err != nil {
			return Scratch{}, fmt.Errorf("creating scratch file: %w", err)
		}
		path := f.Name()
		f.Close()
		os.Remove(path) // sqlite creates it fresh on first open
		return Scratch{
			URL:     "sqlite://" + path,
			Cleanup: func() error { return os.Remove(path) },
		}, nil
	}
	url, name, cleanup, err := container.StartScratchSeries(eng.Name(), series)
	if err != nil {
		return Scratch{}, fmt.Errorf("provisioning scratch database: %w", err)
	}
	return Scratch{URL: url, Container: name, Cleanup: cleanup}, nil
}
