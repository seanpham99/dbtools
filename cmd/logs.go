// cmd/logs.go
package cmd

import (
	"fmt"

	"github.com/seanpham99/dbtools/internal/container"
	"github.com/spf13/cobra"
)

var logsContainer = container.LogsFor

var logsFollow bool

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Stream logs from the tool-owned local database container",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runLogs()
	},
}

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "stream new log output as it's written")
	rootCmd.AddCommand(logsCmd)
}

func runLogs() error {
	cfg, err := loadConfig("dbtools.toml")
	if err != nil {
		return fmt.Errorf("loading dbtools.toml (run 'dbtools init' first?): %w", err)
	}
	engineName := localEngineName(cfg)
	if engineName == "sqlite" {
		return fmt.Errorf("sqlite has no container to show logs for")
	}
	projectID, err := loadProjectID(cfg)
	if err != nil {
		return err
	}
	return logsContainer(engineName, projectID, logsFollow)
}
