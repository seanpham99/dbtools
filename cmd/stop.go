package cmd

import (
	"fmt"

	"github.com/seanpham99/dbtools/internal/container"
	"github.com/seanpham99/dbtools/internal/localenv"
	"github.com/spf13/cobra"
)

var stopContainer = container.StopFor

var removeLocalEnv = localenv.Remove

var stopNoBackup bool

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the tool-owned local database container",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStop()
	},
}

func init() {
	stopCmd.Flags().BoolVar(&stopNoBackup, "no-backup", false, "also delete the container's data volume (today's full-wipe behavior); by default the volume survives for the next start")
	rootCmd.AddCommand(stopCmd)
}

func runStop() error {
	engineName := loadLocalEngineName()
	if engineName == "sqlite" {
		fmt.Println("sqlite needs no server — nothing to stop")
		return nil
	}
	projectID := loadProjectIDOrDefault()
	if err := stopContainer(engineName, projectID, stopNoBackup); err != nil {
		return err
	}
	if err := removeLocalEnv(); err != nil {
		return err
	}
	fmt.Println("local container stopped")
	return nil
}
