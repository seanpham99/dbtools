package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/generate"
	"github.com/spf13/cobra"
)

// stripHeader drops the first two "# ..." comment lines (which carry a
// generation timestamp that legitimately differs between otherwise-identical runs).
func stripHeader(s string) string {
	lines := strings.SplitN(s, "\n", 3)
	if len(lines) < 3 {
		return s
	}
	return lines[2]
}

var (
	generateOut   string
	generateYes   bool
	generateCheck bool
	generateLang  string
	generateZod   bool
)

var generateCmd = &cobra.Command{
	Use:   "generate [target]",
	Short: "Generate pydantic BaseModel classes from live DB schema",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		targetName := "local"
		if len(args) > 0 && args[0] != "" {
			targetName = args[0]
		}
		return runGenerate(targetName)
	},
}

func init() {
	generateCmd.Flags().StringVar(&generateOut, "out", "", "output file path (default from dbtools.toml [generate] out, else db_models.py)")
	generateCmd.Flags().BoolVar(&generateYes, "yes", false, "confirm generating from the prod target")
	generateCmd.Flags().BoolVar(&generateCheck, "check", false, "don't write; exit non-zero if output would differ from the existing file (CI drift check)")
	generateCmd.Flags().StringVar(&generateLang, "lang", "python", "output language: python (pydantic) or ts (TypeScript interfaces + optional zod)")
	generateCmd.Flags().BoolVar(&generateZod, "zod", false, "with --lang ts: also emit zod schemas for each interface")
	rootCmd.AddCommand(generateCmd)
}

func runGenerate(targetName string) error {
	// generate is read-only, but generating from a prod target without
	// --yes is still a surprising thing to do (M5: the old guard only
	// matched the literal name "prod").
	if cfg, err := config.Load("dbtools.toml"); err == nil {
		if t, ok := cfg.Targets[targetName]; ok && t.Protected && !generateYes {
			return fmt.Errorf("refusing to generate from protected target %q without --yes", targetName)
		}
	} else if targetName == "prod" && !generateYes {
		return fmt.Errorf("refusing to generate from %q without --yes", targetName)
	}

	cfg, err := config.Load("dbtools.toml")
	if err != nil {
		return fmt.Errorf("loading dbtools.toml: %w", err)
	}

	url, err := cfg.ResolveURL(targetName)
	if err != nil {
		return err
	}

	eng, err := engine.ForTarget(cfg.EngineName(targetName), url)
	if err != nil {
		return err
	}

	db, err := eng.Open(url)
	if err != nil {
		return err
	}
	defer db.Close()

	tables, unmapped, err := eng.Introspect(db, cfg.Generate.Exclude)
	if err != nil {
		return err
	}
	for _, w := range unmapped {
		fmt.Fprintf(os.Stderr, "warning: no Python type mapping for %s, using Any\n", w)
	}

	if generateLang != "python" && generateLang != "ts" {
		return fmt.Errorf("unsupported --lang %q (want \"python\" or \"ts\")", generateLang)
	}
	if generateZod && generateLang != "ts" {
		return fmt.Errorf("--zod requires --lang ts")
	}

	outContent, err := renderForLang(tables, targetName)
	if err != nil {
		return err
	}

	outPath := generateOut
	if outPath == "" {
		outPath = cfg.Generate.Out
	}
	if outPath == "" {
		switch generateLang {
		case "ts":
			outPath = "db_models.ts"
		default:
			outPath = "db_models.py"
		}
	}

	if generateCheck {
		existing, err := os.ReadFile(outPath)
		if err != nil {
			return fmt.Errorf("reading %s for --check: %w", outPath, err)
		}
		// The header comment carries a generation timestamp that always differs
		// between runs; compare everything below it instead of the raw bytes.
		if stripHeader(string(existing)) == stripHeader(outContent) {
			fmt.Printf("%s is up to date with %s\n", outPath, targetName)
			return nil
		}
		return fmt.Errorf("%s is out of date with %s; run `dbtools generate %s --lang %s%s --out %s` to refresh", outPath, targetName, targetName, generateLang, map[bool]string{true: " --zod", false: ""}[generateZod], outPath)
	}

	if err := os.WriteFile(outPath, []byte(outContent), 0644); err != nil {
		return fmt.Errorf("writing generated output to %s: %w", outPath, err)
	}

	what := fmt.Sprintf("%d pydantic models", len(tables))
	if generateLang == "ts" {
		what = fmt.Sprintf("%d TypeScript interfaces", len(tables))
		if generateZod {
			what += " + zod schemas"
		}
	}
	fmt.Printf("generated %s (%s target) → %s\n", what, targetName, outPath)
	return nil
}

// renderForLang dispatches generate output to the requested language.
func renderForLang(tables []generate.TableSchema, targetName string) (string, error) {
	switch generateLang {
	case "python":
		return generate.Render(tables, targetName)
	case "ts":
		return generate.RenderTS(tables, targetName, generateZod)
	default:
		return "", fmt.Errorf("unsupported --lang %q (want \"python\" or \"ts\")", generateLang)
	}
}
