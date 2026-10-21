package scratchdb

import (
	"fmt"
	"os"

	"github.com/seanpham99/dbtools/internal/engine"
)

// Scratch is a throwaway database and whatever is needed to tear it down.
type Scratch struct {
	// URL connects to the scratch database from this process.
	URL string
	// Container names the Docker container hosting it, or "" when there
	// isn't one — a SQLite tempfile, a database the caller supplied via
	// --against, or a pgx cluster. Callers that need a version-matched
	// engine tool (see dump.Schema) run it inside this container, so a
	// provider with no container must never reach them.
	Container string
	// Cleanup tears the scratch database down. nil when the caller owns it.
	Cleanup func() error
}

// Provision returns a throwaway database for eng in an ephemeral container,
// pinned to the engine's default version series.
func Provision(eng engine.Engine, against string) (Scratch, error) {
	return ProvisionContainer(eng, against, "")
}

// ProvisionContainer provisions the scratch database in an ephemeral
// container, whatever provider the project otherwise uses.
//
// It is the provider that can host the engine's own dump tool inside the
// scratch database, so squash provisions through it directly: the dump
// tool's version has to match the server it dumps, and only the container
// carries a matching tool.
func ProvisionContainer(eng engine.Engine, against, series string) (Scratch, error) {
	return provision(eng, against, series, Docker())
}

// ProvisionSeries provisions the scratch database for a comparison against
// a target on series, choosing the provider that can render that server
// major — see ChooseProvider. The returned note explains a fallback from
// the pgx runtime to a container; it is empty when there is nothing to say.
//
// Callers that compare schemas (diff) should pass the target's series from
// ServerSeries: catalog rendering is not stable across versions, and a
// mismatched scratch database reports that difference as drift that does
// not exist. See ADR 003 decision 5.
func ProvisionSeries(eng engine.Engine, against, series string, projectUsesPgX bool) (Scratch, string, error) {
	// A caller-supplied database and sqlite's tempfile involve no provider,
	// so the selection — and any note about it — would be noise.
	if against != "" || eng.Name() == "sqlite" {
		scratch, err := provision(eng, against, series, Docker())
		return scratch, "", err
	}
	provider, note := ChooseProvider(eng.Name(), series, projectUsesPgX)
	scratch, err := provision(eng, against, series, provider)
	if err != nil && note != "" {
		// The fallback failed — usually because Docker is not there either.
		// Name why the pgx runtime was not usable, or the error reads as if
		// Docker were the only thing that was ever considered.
		return Scratch{}, note, fmt.Errorf("%s; %w", note, err)
	}
	return scratch, note, err
}

// provision handles the cases that need no provider — a caller-supplied
// database, and sqlite's tempfile — and delegates the rest.
func provision(eng engine.Engine, against, series string, p Provider) (Scratch, error) {
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
	return p.Provision(eng, series)
}
