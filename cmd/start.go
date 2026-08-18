package cmd

import (
	"fmt"

	"github.com/dbtools/dbtools/internal/container"
	"github.com/dbtools/dbtools/internal/localenv"
	"github.com/spf13/cobra"
)

var startContainer = container.StartFor

var writeLocalEnv = localenv.Write

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the tool-owned local database container",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStart()
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}

func runStart() error {
	cfg, err := loadConfig("dbtools.toml")
	if err != nil {
		return fmt.Errorf("loading dbtools.toml (run 'dbtools init' first?): %w", err)
	}
	target, ok := cfg.Targets["local"]
	if !ok {
		return fmt.Errorf(`no "local" target configured in dbtools.toml`)
	}

	engineName := localEngineName(cfg)
	if engineName == "sqlite" {
		fmt.Println("sqlite needs no server — nothing to start; run 'dbtools up' directly")
		return nil
	}

	url, err := startContainer(engineName)
	if err != nil {
		return err
	}
	if err := writeLocalEnv(map[string]string{target.URLEnv: url}); err != nil {
		return err
	}
	fmt.Printf("local container started; %s is set via %s\n", target.URLEnv, localenv.Path())
	return nil
}
