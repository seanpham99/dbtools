package cmd

import (
	"fmt"

	"github.com/seanpham99/dbtools/internal/container"
	"github.com/spf13/cobra"
)

var pruneContainers = container.Prune

var (
	pruneVolumes bool
	pruneDryRun  bool
)

var pruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Remove tool-owned containers whose project directory is gone",
	Args:  cobra.NoArgs,
	Long: `Prune removes dbtools-managed containers (labeled dbtools.managed) whose
dbtools.toml no longer exists — the leak left behind when a git worktree is
deleted, where nothing runs dbtools stop.

Resources are only removed when their configpath label proves the owning
project directory is gone; anything ambiguous is skipped. Data volumes are
left alone unless --volumes is passed, since deleting one wipes that
project's local database.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPrune()
	},
}

func init() {
	pruneCmd.Flags().BoolVar(&pruneVolumes, "volumes", false, "also remove the labeled -data volumes of pruned projects (destroys their local databases)")
	pruneCmd.Flags().BoolVar(&pruneDryRun, "dry-run", false, "report what would be removed without removing it")
	rootCmd.AddCommand(pruneCmd)
}

func runPrune() error {
	rep, err := pruneContainers(pruneVolumes, pruneDryRun)
	if err != nil {
		return err
	}
	verb := "removed"
	if pruneDryRun {
		verb = "would remove"
	}
	for _, name := range rep.Containers {
		fmt.Printf("%s container %s (project config gone)\n", verb, name)
	}
	for _, name := range rep.Volumes {
		fmt.Printf("%s volume %s\n", verb, name)
	}
	if len(rep.Containers) == 0 && len(rep.Volumes) == 0 {
		fmt.Println("nothing to prune")
	}
	return nil
}
