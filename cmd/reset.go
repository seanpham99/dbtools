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

// recreateLocalDatabase drops and recreates the tool-owned local
// container's database for eng. reset is deliberately scoped to that
// container — its maintenance URL comes from the container package, never
// from user configuration, so a mistyped target URL can't aim the drop at
// a real server.
func recreateLocalDatabase(eng engine.Engine) error {
	maintenanceURL, err := container.MaintenanceURLFor(eng.Name())
	if err != nil {
		return err
	}
	db, err := eng.Open(maintenanceURL)
	if err != nil {
		return fmt.Errorf("opening maintenance connection: %w", err)
	}
	defer db.Close()

	switch eng.Name() {
	case "mssql":
		query := fmt.Sprintf(`IF DB_ID(N'%s') IS NOT NULL
BEGIN
    ALTER DATABASE %s SET SINGLE_USER WITH ROLLBACK IMMEDIATE;
    DROP DATABASE %s;
END
CREATE DATABASE %s;`, container.DatabaseName, container.DatabaseName, container.DatabaseName, container.DatabaseName)
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("recreating %s: %w", container.DatabaseName, err)
		}
	case "postgres":
		// CREATE/DROP DATABASE cannot run inside a transaction block, so
		// each statement is its own Exec. WITH (FORCE) (PG13+) kicks out
		// lingering connections, mirroring MSSQL's ROLLBACK IMMEDIATE.
		if _, err := db.Exec(fmt.Sprintf(`DROP DATABASE IF EXISTS %s WITH (FORCE)`, container.DatabaseName)); err != nil {
			return fmt.Errorf("dropping %s: %w", container.DatabaseName, err)
		}
		if _, err := db.Exec(fmt.Sprintf(`CREATE DATABASE %s`, container.DatabaseName)); err != nil {
			return fmt.Errorf("creating %s: %w", container.DatabaseName, err)
		}
	default:
		return fmt.Errorf("reset does not support engine %q", eng.Name())
	}
	return nil
}

func runReset() error {
	cfg, err := loadConfig("dbtools.toml")
	if err != nil {
		return fmt.Errorf("loading dbtools.toml: %w", err)
	}

	// Validate the configured local target (URL resolvable, engine matches
	// its scheme) BEFORE the destructive drop/recreate — a misconfigured
	// target must never leave the local database wiped and then fail.
	localURL, err := cfg.ResolveURL("local")
	if err != nil {
		return err
	}
	eng, err := engine.ForTarget(cfg.EngineName("local"), localURL)
	if err != nil {
		return err
	}

	if err := resetLocalDatabase(eng); err != nil {
		return err
	}

	status, err := applyRun(cfg, "local", "")
	if err != nil {
		return err
	}
	fmt.Printf("local: replayed to version %d\n", status.CurrentVersion)

	if err := seedRun(eng, localURL); err != nil {
		return fmt.Errorf("running %s: %w", seed.Filename, err)
	}
	fmt.Printf("%s applied (or skipped if absent)\n", seed.Filename)
	return nil
}
