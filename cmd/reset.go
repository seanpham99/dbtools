package cmd

import (
	"fmt"

	"github.com/dbtools/dbtools/internal/apply"
	"github.com/dbtools/dbtools/internal/config"
	"github.com/dbtools/dbtools/internal/container"
	"github.com/dbtools/dbtools/internal/engine"
	"github.com/dbtools/dbtools/internal/seed"
	"github.com/spf13/cobra"
)

var loadConfig = config.Load

var applyRun = apply.Run

var seedRun = seed.Run

var resetLocalDatabase = recreateLocalDatabase

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Drop, recreate, replay migrations, and run seed.sql against the local database",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runReset()
	},
}

func init() {
	rootCmd.AddCommand(resetCmd)
}

func recreateLocalDatabase() error {
	// reset is deliberately scoped to the tool-owned local MSSQL container
	// (its URL is container.MasterURL(), always mssql://); seeding also runs
	// through the MSSQL path. A future non-MSSQL local target gets its own
	// engine-owned reset/seed capability rather than reusing this one.
	eng, err := engine.ForURL(container.MasterURL())
	if err != nil {
		return err
	}
	db, err := eng.Open(container.MasterURL())
	if err != nil {
		return fmt.Errorf("opening master connection: %w", err)
	}
	defer db.Close()

	query := fmt.Sprintf(`IF DB_ID(N'%s') IS NOT NULL
BEGIN
	ALTER DATABASE %s SET SINGLE_USER WITH ROLLBACK IMMEDIATE;
	DROP DATABASE %s;
END
CREATE DATABASE %s;`, container.DatabaseName, container.DatabaseName, container.DatabaseName, container.DatabaseName)
	if _, err := db.Exec(query); err != nil {
		return fmt.Errorf("recreating %s: %w", container.DatabaseName, err)
	}
	return nil
}

func runReset() error {
	cfg, err := loadConfig("dbtools.toml")
	if err != nil {
		return fmt.Errorf("loading dbtools.toml: %w", err)
	}

	if err := resetLocalDatabase(); err != nil {
		return err
	}

	status, err := applyRun(cfg, "local", "")
	if err != nil {
		return err
	}
	fmt.Printf("local: replayed to version %d\n", status.CurrentVersion)

	localURL, err := cfg.ResolveURL("local")
	if err != nil {
		return err
	}
	if err := seedRun(localURL); err != nil {
		return fmt.Errorf("running %s: %w", seed.Filename, err)
	}
	fmt.Printf("%s applied (or skipped if absent)\n", seed.Filename)
	return nil
}
