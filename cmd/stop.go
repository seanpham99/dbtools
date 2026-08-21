package cmd

import (
	"fmt"

	"github.com/seanpham99/dbtools/internal/container"
	"github.com/seanpham99/dbtools/internal/localenv"
	"github.com/spf13/cobra"
)

var stopContainer = container.StopFor

var removeLocalEnv = localenv.Remove

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop and remove the tool-owned local database container",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStop()
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}

func runStop() error {
	engineName := loadLocalEngineName()
	if engineName == "sqlite" {
		fmt.Println("sqlite needs no server — nothing to stop")
		return nil
	}
	if err := stopContainer(engineName); err != nil {
		return err
	}
	if err := removeLocalEnv(); err != nil {
		return err
	}
	fmt.Println("local container stopped")
	return nil
}
