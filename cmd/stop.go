package cmd

import (
	"fmt"

	"github.com/seanpham99/dbtools/internal/localenv"
	"github.com/spf13/cobra"
)

var removeLocalEnv = localenv.Remove

var stopNoBackup bool

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the tool-owned local database",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStop()
	},
}

func init() {
	stopCmd.Flags().BoolVar(&stopNoBackup, "no-backup", false, "also delete the local database's data (today's full-wipe behavior); by default the data survives for the next start")
	rootCmd.AddCommand(stopCmd)
}

func runStop() error {
	cfg := localConfigOrNil()
	engineName := loadLocalEngineName()
	if engineName == "sqlite" {
		fmt.Println("sqlite needs no server — nothing to stop")
		return nil
	}
	projectID := loadProjectIDOrDefault()
	if err := stopLocalDB(cfg, engineName, projectID, stopNoBackup); err != nil {
		return err
	}
	if err := removeLocalEnv(); err != nil {
		return err
	}
	fmt.Println("local database stopped")
	return nil
}
