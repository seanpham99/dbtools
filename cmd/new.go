package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/scaffold"
	"github.com/spf13/cobra"
)

var newCmd = &cobra.Command{
	Use:   "new [name]",
	Short: "Create a new empty migration file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path, err := runNew(time.Now(), args[0])
		if err != nil {
			return err
		}
		fmt.Println("created", path)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(newCmd)
}

func runNew(now time.Time, name string) (string, error) {
	cfg, err := config.Load("dbtools.toml")
	if err != nil {
		return "", fmt.Errorf("loading dbtools.toml (run 'dbtools init' first?): %w", err)
	}

	filename, err := scaffold.NextUpFilename(now, cfg.MigrationsDir, cfg.Migrations.UpSuffix, name)
	if err != nil {
		return "", fmt.Errorf("determining next migration filename: %w", err)
	}
	path := filepath.Join(cfg.MigrationsDir, filename)

	if err := os.MkdirAll(cfg.MigrationsDir, 0o755); err != nil {
		return "", fmt.Errorf("creating migrations dir: %w", err)
	}

	// os.Root scopes the create to the migrations dir, so even a filename
	// that slips past name validation cannot land outside it.
	root, err := os.OpenRoot(cfg.MigrationsDir)
	if err != nil {
		return "", fmt.Errorf("opening migrations dir: %w", err)
	}
	defer root.Close()

	f, err := root.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", fmt.Errorf("writing migration file: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString("-- " + name + "\n"); err != nil {
		return "", fmt.Errorf("writing migration content: %w", err)
	}
	return path, nil
}
