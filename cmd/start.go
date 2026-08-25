package cmd

import (
	"fmt"
	"time"

	"github.com/seanpham99/dbtools/internal/container"
	"github.com/seanpham99/dbtools/internal/localenv"
	"github.com/spf13/cobra"
)

var startContainer = container.StartForWithTimeout

var writeLocalEnv = localenv.Write

var (
	startTimeout time.Duration
	startNoWait  bool
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the tool-owned local database container",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStart()
	},
}

func init() {
	startCmd.Flags().DurationVar(&startTimeout, "timeout", 30*time.Second, "maximum time to wait for the database engine to become ready")
	startCmd.Flags().BoolVar(&startNoWait, "no-wait", false, "return immediately after starting container without waiting for database readiness")
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

	projectID, err := loadProjectID(cfg)
	if err != nil {
		return err
	}

	if !startNoWait {
		fmt.Println("waiting for database engine to accept connections...")
	}
	url, err := startContainer(engineName, projectID, configuredContainerPort(cfg), startTimeout, !startNoWait)
	if err != nil {
		return err
	}
	if err := writeLocalEnv(map[string]string{target.URLEnv: url}); err != nil {
		return err
	}
	fmt.Printf("local container started; %s is set via %s\n", target.URLEnv, localenv.Path())
	return nil
}
