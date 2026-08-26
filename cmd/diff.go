package cmd

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/seanpham99/dbtools/internal/diff"
	"github.com/spf13/cobra"
)

var diffCmd = &cobra.Command{
	Use:   "diff [target]",
	Short: "Replay migrations into a scratch database and compare structurally against the live target (read-only)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = false
		err := runDiff(args[0])
		var exitErr *ExitCodeError
		if errors.As(err, &exitErr) {
			cmd.SilenceUsage = true
		}
		return err
	},
}

var diffAgainst string

func init() {
	diffCmd.Flags().StringVar(&diffAgainst, "against", "", "connection string of an already-provisioned, empty scratch database (skips automatic container provisioning)")
	rootCmd.AddCommand(diffCmd)
}

func runDiff(targetName string) error {
	cfg, err := loadConfig("dbtools.toml")
	if err != nil {
		return fmt.Errorf("loading dbtools.toml: %w", err)
	}

	findings, notes, err := diff.Run(cfg, targetName, diffAgainst)
	if err != nil {
		return err
	}

	if jsonOutput {
		if findings == nil {
			findings = []diff.Finding{}
		}
		if notes == nil {
			notes = []string{}
		}
		b, err := json.Marshal(struct {
			Target   string         `json:"target"`
			Findings []diff.Finding `json:"findings"`
			Notes    []string       `json:"notes,omitempty"`
		}{Target: targetName, Findings: findings, Notes: notes})
		if err != nil {
			return err
		}
		fmt.Println(string(b))
	} else {
		if len(findings) == 0 {
			fmt.Printf("%s: no structural differences\n", targetName)
		}
		for _, f := range findings {
			fmt.Printf("%-8s %-14s %-30s %s\n", f.Kind, f.Object, f.Table+"."+f.Name, f.Detail)
		}
		for _, n := range notes {
			fmt.Printf("note: %s\n", n)
		}
	}

	if len(findings) > 0 {
		return ExitCode(2, fmt.Sprintf("%d structural difference(s) found", len(findings)))
	}
	return nil
}
