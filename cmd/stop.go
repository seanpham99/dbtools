package cmd

import (
	"fmt"

	"github.com/dbtools/dbtools/internal/container"
	"github.com/dbtools/dbtools/internal/localenv"
	"github.com/spf13/cobra"
)

var stopContainer = container.Stop

var removeLocalEnv = localenv.Remove

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop and remove the tool-owned local MSSQL container",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStop()
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}

func runStop() error {
	if err := stopContainer(); err != nil {
		return err
	}
	if err := removeLocalEnv(); err != nil {
		return err
	}
	fmt.Println("local container stopped")
	return nil
}
