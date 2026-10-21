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
	Short: "Remove tool-owned local databases whose project directory is gone",
	Args:  cobra.NoArgs,
	Long: `Prune removes dbtools-managed local databases (labeled dbtools.managed containers and
pgx clusters) whose dbtools.toml no longer exists — the leak left behind when a git
worktree is deleted, where nothing runs dbtools stop.

Resources are only removed when their owner proves the owning project directory
is gone; anything ambiguous is skipped. Container data volumes are left alone
unless --volumes is passed, since deleting one wipes that project's local
database. A pgx cluster holds its runtime and its data together, so --volumes
is what removes one; without it the runtime is stopped and the data is kept.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runPrune()
	},
}

func init() {
	pruneCmd.Flags().BoolVar(&pruneVolumes, "volumes", false, "also remove the local database's data of pruned projects (destroys their local databases)")
	pruneCmd.Flags().BoolVar(&pruneDryRun, "dry-run", false, "report what would be removed without removing it")
	rootCmd.AddCommand(pruneCmd)
}

func runPrune() error {
	verb := "removed"
	if pruneDryRun {
		verb = "would remove"
	}

	// The pgx cleanup runs first: a host without Docker can still hold
	// pgx clusters, and the work is done even when the container half of
	// the command cannot run.
	pgxReport, err := pruneLocalDB(pruneVolumes, pruneDryRun)
	if err != nil {
		return err
	}
	if pgxReport.Skipped {
		fmt.Printf("skipped pgx cluster cleanup: %s\n", pgxReport.Reason)
	}
	for _, path := range pgxReport.Clusters {
		fmt.Printf("%s pgx cluster %s (project config gone)\n", verb, path)
	}
	for _, path := range pgxReport.StoppedClusters {
		if pruneDryRun {
			fmt.Printf("would stop pgx cluster %s (project config gone; data kept)\n", path)
			continue
		}
		fmt.Printf("stopped pgx cluster %s (project config gone; data kept)\n", path)
	}

	rep, err := pruneContainers(pruneVolumes, pruneDryRun)
	if err != nil {
		// A missing Docker is not a failure of this command when pgx
		// clusters were the only thing to clean up. The note tells the
		// user what was skipped.
		if container.IsDockerUnavailable(err) {
			fmt.Println("skipped container cleanup: Docker is not available on this machine")
			return nil
		}
		return err
	}
	for _, name := range rep.Containers {
		fmt.Printf("%s container %s (project config gone)\n", verb, name)
	}
	for _, name := range rep.Volumes {
		fmt.Printf("%s volume %s\n", verb, name)
	}
	if len(rep.Containers) == 0 && len(rep.Volumes) == 0 && len(pgxReport.Clusters) == 0 && len(pgxReport.StoppedClusters) == 0 {
		fmt.Println("nothing to prune")
	}
	return nil
}
