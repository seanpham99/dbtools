package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dbtools/dbtools/internal/config"
	"github.com/dbtools/dbtools/internal/scaffold"
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

	filename := scaffold.UpFilename(now, name)
	path := filepath.Join(cfg.MigrationsDir, filename)

	if err := os.WriteFile(path, []byte("-- "+name+"\n"), 0o644); err != nil {
		return "", fmt.Errorf("writing migration file: %w", err)
	}
	return path, nil
}
