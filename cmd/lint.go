package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/migrator"
	"github.com/spf13/cobra"
)

var (
	lintDirFlag  string
	lintJSONFlag bool
)

var lintCmd = &cobra.Command{
	Use:   "lint",
	Short: "Validate migration filenames, version numbers, and file hygiene",
	Long:  "Statically checks migration files for duplicate version prefixes, invalid filename patterns, and empty files without connecting to a database.",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := lintDirFlag
		if dir == "" {
			cfg, err := config.Load("dbtools.toml")
			if err != nil {
				// Fallback to default "migrations" if dbtools.toml is absent
				dir = "migrations"
			} else {
				dir = cfg.MigrationsDir
			}
		}

		report, err := migrator.Lint(dir)
		if err != nil {
			return err
		}

		if jsonOutput || lintJSONFlag {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(report); err != nil {
				return err
			}
		} else {
			if !report.HasErrors() {
				fmt.Printf("✓ %d migration files in %q checked, 0 issues found.\n", report.Total, dir)
			} else {
				fmt.Fprintf(os.Stderr, "Found %d issue(s) in %q:\n\n", len(report.Findings), dir)
				for _, f := range report.Findings {
					fmt.Fprintf(os.Stderr, "  • [%s] %s: %s\n", f.Rule, f.File, f.Message)
				}
			}
		}

		if report.HasErrors() {
			return ExitCode(1, "")
		}
		return nil
	},
}

func init() {
	lintCmd.Flags().StringVar(&lintDirFlag, "dir", "", "Path to migrations directory (defaults to migrations_dir from dbtools.toml)")
	lintCmd.Flags().BoolVar(&lintJSONFlag, "json", false, "Output results in JSON format")
	rootCmd.AddCommand(lintCmd)
}
