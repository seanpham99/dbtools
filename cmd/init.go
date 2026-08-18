package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const defaultConfigTemplate = `# dbtools.toml
# Targets name a database environment. Each target's connection string is
# resolved at runtime from the named environment variable — never write a
# literal connection string in this file.

migrations_dir = "migrations"

[targets.local]
url_env = "DBTOOLS_LOCAL_URL"
# engine is optional and defaults to the connection URL's scheme
# (e.g. mssql:// -> mssql, postgres:// -> postgres, sqlite:// -> sqlite).
# When set, it must
# match that scheme. Supported engines: mssql, postgres, sqlite.
# sqlite URLs name a file path (sqlite://relative/or/absolute/path.db);
# no server is needed — start/stop are no-ops and reset recreates the file.
engine = "mssql"
`

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a starter dbtools.toml and migrations/ directory",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runInit()
	},
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit() error {
	if _, err := os.Stat("dbtools.toml"); os.IsNotExist(err) {
		if err := os.WriteFile("dbtools.toml", []byte(defaultConfigTemplate), 0o644); err != nil {
			return fmt.Errorf("writing dbtools.toml: %w", err)
		}
		fmt.Println("created dbtools.toml")
	} else {
		fmt.Println("dbtools.toml already exists, leaving it unchanged")
	}

	if err := os.MkdirAll("migrations", 0o755); err != nil {
		return fmt.Errorf("creating migrations directory: %w", err)
	}
	return nil
}
