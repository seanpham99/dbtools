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
	"github.com/seanpham99/dbtools/internal/logger"
	"github.com/seanpham99/dbtools/internal/seed"
	"github.com/spf13/cobra"
)

var loadConfig = config.Load

var applyRun = apply.Run

var seedRun = seed.Run

var resetLocalDatabase = recreateLocalDatabase

var (
	resetYes    bool
	resetTarget string
)

var resetCmd = &cobra.Command{
	Use:   "reset [target]",
	Short: "Drop, recreate, replay migrations, and run seed.sql against an unprotected target",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := resetTarget
		if len(args) > 0 && args[0] != "" {
			target = args[0]
		}
		if target == "" {
			target = "local"
		}
		if !resetYes {
			return fmt.Errorf("refusing to drop and recreate target %q database without --yes", target)
		}
		return runReset(target)
	},
}

func init() {
	resetCmd.Flags().StringVar(&resetTarget, "target", "local", "target database to reset (default: local)")
	resetCmd.Flags().BoolVar(&resetYes, "yes", false, "confirm the destructive drop/recreate of the target database")
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

	maintenanceURL, err := container.MaintenanceURLFor(eng.Name(), localURL)
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
	// A planted symlink must not redirect the remove/recreate cycle at
	// some other file's inode — reset is allowed to clobber the database
	// file, never whatever the symlink points at.
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink; refusing to reset it", path)
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

func runReset(targetNames ...string) error {
	targetName := "local"
	if len(targetNames) > 0 && targetNames[0] != "" {
		targetName = targetNames[0]
	}
	cfg, err := loadConfig("dbtools.toml")
	if err != nil {
		return fmt.Errorf("loading dbtools.toml: %w", err)
	}

	if err := requireUnprotected(cfg, targetName); err != nil {
		return err
	}

	// Validate the configured target (URL resolvable, engine matches
	// its scheme) BEFORE the destructive drop/recreate — a misconfigured
	// target must never leave the database wiped and then fail.
	targetURL, err := cfg.ResolveURL(targetName)
	if err != nil {
		return err
	}
	eng, err := engine.ForTarget(cfg.EngineName(targetName), targetURL)
	if err != nil {
		return err
	}

	if err := resetLocalDatabase(eng, targetURL); err != nil {
		return err
	}

	status, err := applyRun(cfg, targetName, "")
	if err != nil {
		return err
	}
	seedErr := seedRun(eng, targetURL)
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
			Target:          targetName,
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

	logger.Infof("%s: replayed to version %d", targetName, status.CurrentVersion)
	logger.Infof("%s applied (or skipped if absent)", seed.Filename)
	return nil
}
