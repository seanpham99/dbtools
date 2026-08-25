// cmd/restart.go
package cmd

import (
	"fmt"
	"time"

	"github.com/seanpham99/dbtools/internal/container"
	"github.com/seanpham99/dbtools/internal/localenv"
	"github.com/spf13/cobra"
)

var restartContainer = container.RestartForWithTimeout

var (
	restartTimeout time.Duration
	restartNoWait  bool
)

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the tool-owned local database container, preserving its data",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runRestart()
	},
}

func init() {
	restartCmd.Flags().DurationVar(&restartTimeout, "timeout", 30*time.Second, "maximum time to wait for the database engine to become ready")
	restartCmd.Flags().BoolVar(&restartNoWait, "no-wait", false, "return immediately after restarting container without waiting for database readiness")
	rootCmd.AddCommand(restartCmd)
}

func runRestart() error {
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
		fmt.Println("sqlite needs no server — nothing to restart")
		return nil
	}

	projectID, err := loadProjectID(cfg)
	if err != nil {
		return err
	}

	url, err := restartContainer(engineName, projectID, configuredContainerPort(cfg), restartTimeout, !restartNoWait)
	if err != nil {
		return err
	}
	if err := writeLocalEnv(map[string]string{target.URLEnv: url}); err != nil {
		return err
	}
	fmt.Printf("local container restarted; %s is set via %s\n", target.URLEnv, localenv.Path())
	return nil
}
