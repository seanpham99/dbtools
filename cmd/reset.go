package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/seanpham99/dbtools/internal/apply"
	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/container"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/engine/sqliteengine"
	"github.com/seanpham99/dbtools/internal/seed"
	"github.com/spf13/cobra"
)

var loadConfig = config.Load

var applyRun = apply.Run

var seedRun = seed.Run

var resetLocalDatabase = recreateLocalDatabase

var resetYes bool

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Drop, recreate, replay migrations, and run seed.sql against the local database",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !resetYes {
			return fmt.Errorf("refusing to drop and recreate the local database without --yes")
		}
		return runReset()
	},
}

func init() {
	resetCmd.Flags().BoolVar(&resetYes, "yes", false, "confirm the destructive drop/recreate of the local database")
	rootCmd.AddCommand(resetCmd)
}

// recreateLocalDatabase drops and recreates the tool-owned local
// container's database for eng. For server engines, reset is deliberately
// scoped to that container — its maintenance URL comes from the container
// package, never from user configuration, so a mistyped target URL can't
// aim the drop at a real server. For sqlite there is no server: the local
// target's own database file is deleted and recreated empty.
func recreateLocalDatabase(eng engine.Engine, localURL string) error {
	if eng.Name() == "sqlite" {
		return recreateSQLiteFile(localURL)
	}

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

// recreateSQLiteFile deletes the sqlite database file named by localURL
// (plus its -wal/-shm sidecars) and recreates it empty, so the following
// migration replay starts from a truly blank database.
func recreateSQLiteFile(localURL string) error {
	path := sqliteengine.PathFromURL(localURL)
	if path == "" {
		return fmt.Errorf("sqlite URL %q has no file path", localURL)
	}
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing %s: %w", p, err)
		}
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("recreating %s: %w", path, err)
	}
	return f.Close()
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

	if err := resetLocalDatabase(eng, localURL); err != nil {
		return err
	}

	status, err := applyRun(cfg, "local", "")
	if err != nil {
		return err
	}
	seedErr := seedRun(eng, localURL)
	if seedErr != nil {
		return fmt.Errorf("running %s: %w", seed.Filename, seedErr)
	}

	if jsonOutput {
		b, err := json.Marshal(struct {
			Target          string `json:"target"`
			ReplayedVersion uint64 `json:"replayed_version"`
			HasVersion      bool   `json:"has_version"`
			SeedApplied     bool   `json:"seed_applied"`
		}{
			Target:          "local",
			ReplayedVersion: status.CurrentVersion,
			HasVersion:      status.HasVersion,
			SeedApplied:     true,
		})
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}

	fmt.Printf("local: replayed to version %d\n", status.CurrentVersion)
	fmt.Printf("%s applied (or skipped if absent)\n", seed.Filename)
	return nil
}
