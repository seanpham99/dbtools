package cmd

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dbtools/dbtools/internal/config"
	"github.com/dbtools/dbtools/internal/dashboard"
	"github.com/dbtools/dbtools/internal/statusinfo"
	"github.com/spf13/cobra"
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Launch a read-only TUI showing migration status for every target",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDashboard()
	},
}

func init() {
	rootCmd.AddCommand(dashboardCmd)
}

func runDashboard() error {
	cfg, err := config.Load("dbtools.toml")
	if err != nil {
		return fmt.Errorf("loading dbtools.toml: %w", err)
	}

	program := tea.NewProgram(dashboard.NewModel(cfg, statusinfo.Collect))
	if _, err := program.Run(); err != nil {
		return err
	}
	return nil
}
