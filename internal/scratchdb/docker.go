package scratchdb

import (
	"fmt"

	"github.com/seanpham99/dbtools/internal/container"
	"github.com/seanpham99/dbtools/internal/engine"
)

// dockerProvider provisions the scratch database in an ephemeral
// container. Its image is pinned to the target's version series, which is
// what keeps both sides of a comparison on one server major.
type dockerProvider struct{}

func (dockerProvider) Name() string { return "docker" }

func (dockerProvider) Provision(eng engine.Engine, series string) (Scratch, error) {
	url, name, cleanup, err := container.StartScratchSeries(eng.Name(), series)
	if err != nil {
		return Scratch{}, fmt.Errorf("provisioning scratch database: %w", err)
	}
	return Scratch{URL: url, Container: name, Cleanup: cleanup}, nil
}
