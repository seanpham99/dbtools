package cmd

import (
	"fmt"
	"os"

	// Engine implementations self-register with internal/engine in their
	// package init(); every registered engine's commands work from here.
	_ "github.com/seanpham99/dbtools/internal/engine/mssqlengine"
	_ "github.com/seanpham99/dbtools/internal/engine/mysqlengine"
	_ "github.com/seanpham99/dbtools/internal/engine/postgresengine"
	_ "github.com/seanpham99/dbtools/internal/engine/sqliteengine"
	"github.com/seanpham99/dbtools/internal/localenv"
	"github.com/seanpham99/dbtools/internal/logger"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "dbtools",
	Short: "dbtools manages MSSQL/Postgres schema migrations and local dev databases",
}

var (
	jsonOutput bool
	logFormat  string
)

func loadLocalEnv() error {
	vars, err := localenv.Load()
	if err != nil {
		return err
	}
	for key, value := range vars {
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("setting %s from %s: %w", key, localenv.Path(), err)
		}
	}
	return nil
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "emit machine-readable JSON output")
	rootCmd.PersistentFlags().StringVar(&logFormat, "log-format", "text", "log format (text, json)")
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		fmtVal := logFormat
		if !cmd.Flags().Changed("log-format") {
			if env := os.Getenv("DBTOOLS_LOG_FORMAT"); env != "" {
				fmtVal = env
			}
		}
		switch fmtVal {
		case "text", "json":
		default:
			return fmt.Errorf("invalid --log-format %q (must be 'text' or 'json')", fmtVal)
		}
		logger.SetFormat(fmtVal)
		if jsonOutput {
			logger.SetOutput(os.Stderr)
		}
		return loadLocalEnv()
	}
}

// Execute runs the dbtools root command. Called from main.go.
func Execute() error {
	return rootCmd.Execute()
}
