package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/seanpham99/dbtools/internal/clone"
	"github.com/spf13/cobra"
)

var cloneRun = clone.Run

var (
	cloneYes    bool
	cloneMask   bool
	cloneNoMask bool
	cloneLimit  int
	cloneWhere  string
)

var cloneCmd = &cobra.Command{
	Use:   "clone <source> <dest>",
	Short: "Copy data from source target into dest target, masking sensitive columns by default",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if cloneMask && cloneNoMask {
			return fmt.Errorf("--mask and --no-mask are mutually exclusive")
		}
		if !cloneYes {
			return fmt.Errorf("clone overwrites every cloned table in %q — pass --yes to confirm", args[1])
		}
		return runClone(args[0], args[1])
	},
}

func init() {
	cloneCmd.Flags().BoolVar(&cloneYes, "yes", false, "confirm overwriting dest's data")
	cloneCmd.Flags().BoolVar(&cloneMask, "mask", false, "mask sensitive columns (default; --no-mask opts out)")
	cloneCmd.Flags().BoolVar(&cloneNoMask, "no-mask", false, "copy sensitive columns unmasked — a documented PII risk")
	cloneCmd.Flags().IntVar(&cloneLimit, "limit", 0, "copy at most this many rows per table (0 = no limit)")
	cloneCmd.Flags().StringVar(&cloneWhere, "where", "", "SQL filter appended to every table's SELECT (trusted, unsanitized)")
	rootCmd.AddCommand(cloneCmd)
}

func runClone(sourceTarget, destTarget string) error {
	cfg, err := loadConfig("dbtools.toml")
	if err != nil {
		return fmt.Errorf("loading dbtools.toml: %w", err)
	}
	// Clone is exactly as destructive to dest as reset is — no --yes
	// override for a protected dest, unlike push's softer rule. See this
	// plan's Global Constraints for why.
	if err := requireUnprotected(cfg, destTarget); err != nil {
		return err
	}

	result, err := cloneRun(cfg, sourceTarget, destTarget, clone.Options{
		Mask:  !cloneNoMask,
		Limit: cloneLimit,
		Where: cloneWhere,
	})
	if err != nil {
		return err
	}

	if jsonOutput {
		b, err := json.Marshal(result)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}

	fmt.Printf("%s -> %s: cloned %d table(s)\n", result.Source, result.Dest, len(result.Tables))
	for _, tr := range result.Tables {
		maskNote := ""
		if len(tr.MaskedColumns) > 0 {
			maskNote = fmt.Sprintf(" (masked: %v)", tr.MaskedColumns)
		}
		fmt.Printf("  %-20s %d row(s)%s\n", tr.Table, tr.RowsCopied, maskNote)
	}
	return nil
}
