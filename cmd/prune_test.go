package cmd

import (
	"errors"
	"testing"

	"github.com/seanpham99/dbtools/internal/container"
)

func TestPruneCmd_PassesFlags(t *testing.T) {
	var gotVolumes, gotDryRun bool
	orig := pruneContainers
	pruneContainers = func(volumes, dryRun bool) (container.PruneReport, error) {
		gotVolumes, gotDryRun = volumes, dryRun
		return container.PruneReport{Containers: []string{"dbtools-postgres-deadbeef"}}, nil
	}
	t.Cleanup(func() { pruneContainers = orig })

	oldV, oldD := pruneVolumes, pruneDryRun
	pruneVolumes, pruneDryRun = true, true
	t.Cleanup(func() { pruneVolumes, pruneDryRun = oldV, oldD })

	if err := runPrune(); err != nil {
		t.Fatal(err)
	}
	if !gotVolumes || !gotDryRun {
		t.Fatalf("flags not plumbed: volumes=%v dryRun=%v", gotVolumes, gotDryRun)
	}
}

func TestPruneCmd_PropagatesError(t *testing.T) {
	orig := pruneContainers
	pruneContainers = func(volumes, dryRun bool) (container.PruneReport, error) {
		return container.PruneReport{}, errors.New("docker daemon gone")
	}
	t.Cleanup(func() { pruneContainers = orig })

	if err := runPrune(); err == nil {
		t.Fatal("expected error, got nil")
	}
}
