package scratchdb

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/pgx"
)

// ScratchDatabaseName is the database created inside a scratch cluster.
const ScratchDatabaseName = "dbtools_scratch"

// pgxScratchTimeout bounds each pgx subprocess sequence the scratch
// provider runs. Nothing here takes a caller context, so without a bound a
// hung binary would hold diff open with no cancellation path.
const pgxScratchTimeout = 2 * time.Minute

func scratchContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), pgxScratchTimeout)
}

// pgxProvider provisions the scratch database in an ephemeral pgx cluster.
//
// Each invocation gets its own cluster, with its own identity and its own
// free port, so two comparisons never share a database and a teardown can
// only remove the cluster it created. The cluster is initialised with
// ephemeral durability: its data is throwaway, so it does not need the
// crash durability a project's local database promises.
type pgxProvider struct{}

func (pgxProvider) Name() string { return "pgx" }

func (pgxProvider) Provision(eng engine.Engine, series string) (Scratch, error) {
	if eng.Name() != "postgres" {
		return Scratch{}, fmt.Errorf("the pgx scratch provider serves the postgres engine only, but the comparison uses %s", eng.Name())
	}
	client, err := pgx.NewClient(eng.Name(), scratchID())
	if err != nil {
		return Scratch{}, err
	}
	client.Durability = "ephemeral"
	ctx, cancel := scratchContext()
	defer cancel()
	if err := client.EnsureRunning(ctx, ""); err != nil {
		_ = destroyScratch(client)
		return Scratch{}, fmt.Errorf("provisioning a pgx scratch cluster: %w", err)
	}
	// Record the owning project: a dbtools process killed before its
	// teardown runs leaves the cluster behind, and this is what lets
	// `dbtools prune` judge it. A live comparison keeps its cluster in the
	// runtime, which prune never touches.
	if err := client.WriteOwner(scratchOwnerPath()); err != nil {
		_ = destroyScratch(client)
		return Scratch{}, err
	}
	if err := client.EnsureDatabase(ctx, ScratchDatabaseName); err != nil {
		_ = destroyScratch(client)
		return Scratch{}, fmt.Errorf("creating the pgx scratch database: %w", err)
	}
	url, err := client.DatabaseURL(ctx, ScratchDatabaseName)
	if err != nil {
		_ = destroyScratch(client)
		return Scratch{}, err
	}
	return Scratch{
		URL:     url,
		Cleanup: func() error { return destroyScratch(client) },
	}, nil
}

// scratchID returns a cluster identity that no project's identity collides
// with in practice: project identities are a path hash or a configured
// name, while this carries the process and the nanosecond.
func scratchID() string {
	return fmt.Sprintf("diff-%d-%d", os.Getpid(), time.Now().UnixNano())
}

// scratchOwnerPath returns the dbtools.toml the scratch cluster belongs to,
// which is what prune stats before removing it.
func scratchOwnerPath() string {
	abs, err := filepath.Abs("dbtools.toml")
	if err != nil {
		return ""
	}
	return abs
}

// destroyScratch tears a cluster down under its own bounded context, so a
// cleanup after a failed provisioning cannot inherit a nearly expired one
// and cannot hang the process.
func destroyScratch(client *pgx.Client) error {
	ctx, cancel := scratchContext()
	defer cancel()
	return client.Destroy(ctx)
}
