package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print dbtools version and build information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("dbtools version %s (commit: %s, date: %s)\n", version, commit, date)
	},
}

func init() {
	rootCmd.Version = version
	rootCmd.AddCommand(versionCmd)
}
