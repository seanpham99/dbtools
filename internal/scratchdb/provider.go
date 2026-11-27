package scratchdb

import (
	"fmt"

	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/pgx"
)

// Provider provisions the throwaway database a schema comparison runs
// against.
//
// Two implementations exist. The container provider runs an ephemeral
// container from a version-pinned image; the pgx provider runs an ephemeral
// pgx cluster. Which one is usable is decided by version parity, not by
// preference: a comparison rendered by two different server majors reports
// the difference as drift (ADR 003 decision 5).
type Provider interface {
	// Name identifies the provider in messages.
	Name() string
	// Provision creates a scratch database for eng pinned to series and
	// returns it with its teardown.
	Provision(eng engine.Engine, series string) (Scratch, error)
}

// Docker is the container-backed scratch provider. It can serve any series
// it has an image tag for (postgres:<series>-alpine), so it is the provider
// every engine and every version falls back to.
func Docker() Provider { return dockerProvider{} }

// PgX is the pgx-runtime-backed scratch provider. The runtime serves one
// PostgreSQL major only, so it is usable only for a target on that major.
func PgX() Provider { return pgxProvider{} }

// ChooseProvider returns the provider that provisions the scratch database
// for a comparison against a target on series.
//
// The project's own choice decides whether the pgx runtime is considered at
// all: pgx is opt-in through [local] provider, so a project that never
// selected it keeps the container scratch even on a machine that happens to
// have the binary installed. When the project did select it, the runtime is
// used only if it can render the target's server major — the target must be
// postgres, its major must be known, and it must equal the major the
// installed runtime serves.
//
// The second return value explains a fallback from the pgx runtime to a
// container, so the caller can report it; it is empty when there is nothing
// to explain.
func ChooseProvider(engineName, series string, projectUsesPgX bool) (Provider, string) {
	if !projectUsesPgX {
		return Docker(), ""
	}
	if !pgx.Available() {
		return Docker(), "the pgx runtime is not installed; the scratch database runs in a Docker container instead"
	}
	major, err := pgx.ServerMajor()
	return chooseWith(engineName, series, major, err)
}

// chooseWith is ChooseProvider with the runtime's facts supplied, so the
// selection table is testable without a pgx installation.
func chooseWith(engineName, series, pgxMajor string, pgxErr error) (Provider, string) {
	if engineName != "postgres" {
		return Docker(), fmt.Sprintf("the pgx runtime serves the postgres engine only, but the comparison uses %s; the scratch database runs in a Docker container instead", engineName)
	}
	if series == "" {
		return Docker(), "the target's PostgreSQL version could not be determined; the scratch database runs in a Docker container instead"
	}
	if pgxErr != nil {
		return Docker(), fmt.Sprintf("the pgx runtime's PostgreSQL version could not be read (%v); the scratch database runs in a Docker container instead", pgxErr)
	}
	if pgxMajor == "" {
		return Docker(), ""
	}
	if series != pgxMajor {
		return Docker(), fmt.Sprintf("the pgx runtime serves PostgreSQL %s only and the target is PostgreSQL %s; the scratch database runs in a Docker container instead, so both sides are rendered by the same server major", pgxMajor, series)
	}
	return PgX(), ""
}
