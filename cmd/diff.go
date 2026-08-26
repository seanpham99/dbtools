package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/seanpham99/dbtools/internal/diff"
	"github.com/seanpham99/dbtools/internal/engine/sqliteengine"
	"github.com/seanpham99/dbtools/internal/migrator"
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
	if diffAgainst != "" {
		targetURL, err := cfg.ResolveURLOrFlag(targetName, "")
		if err != nil {
			return err
		}
		if sameDatabase(diffAgainst, targetURL) {
			return errors.New("--against identifies the same database as the target — diff would replay migrations and write the ledger onto the live target")
		}
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
			object := f.Table
			if f.Name != "" {
				object += "." + f.Name
			}
			fmt.Printf("%-8s %-14s %-30s %s\n", f.Kind, f.Object, object, f.Detail)
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

var mysqlTCPHostPortRE = regexp.MustCompile(`tcp\(([^:]*):(\d+)\)`)

// diffSchemeAliases mirrors internal/engine's own scheme normalization —
// duplicated here rather than exported, since it's one map entry and
// engine.go's version is deliberately unexported.
var diffSchemeAliases = map[string]string{"postgresql": "postgres"}

// sameDatabase reports whether a and b identify the same physical
// database, not merely the same URL string — the guard that stops
// --against from silently aiming at the live target under a
// differently-formatted URL (a real safety issue: diff.Run would then
// replay migrations and write the ledger onto the target it's supposed
// to only ever read). Returns true immediately for an exact string
// match; otherwise both URLs must parse and normalize to the same
// identity. An unparseable URL is treated as "different" here (the
// strings already didn't match), not as a reason to skip the check. This
// is not a full network-identity resolver — two different hostnames that
// happen to resolve to the same server (e.g. via /etc/hosts, a load
// balancer, or a Docker network alias) are NOT detected as equivalent;
// only the common formatting differences (postgres/postgresql scheme,
// 127.0.0.1/::1/localhost, relative vs absolute sqlite paths) are
// collapsed.
func sameDatabase(a, b string) bool {
	if a == b {
		return true
	}
	na, oka := normalizeDatabaseIdentity(a)
	nb, okb := normalizeDatabaseIdentity(b)
	if !oka || !okb {
		return false
	}
	return na == nb
}

func normalizeHost(h string) string {
	h = strings.ToLower(h)
	if h == "127.0.0.1" || h == "::1" {
		return "localhost"
	}
	return h
}

// normalizeDatabaseIdentity extracts a comparable (scheme, host, port,
// database) identity from a connection URL. ok is false when raw can't
// be parsed at all.
func normalizeDatabaseIdentity(raw string) (identity string, ok bool) {
	scheme := migrator.SchemeOf(raw)
	if canonical, exists := diffSchemeAliases[scheme]; exists {
		scheme = canonical
	}

	switch scheme {
	case "sqlite":
		abs, err := filepath.Abs(sqliteengine.PathFromURL(raw))
		if err != nil {
			return "", false
		}
		return "sqlite|" + abs, true
	case "mysql":
		m := mysqlTCPHostPortRE.FindStringSubmatch(raw)
		if m == nil {
			return "", false
		}
		host, port := normalizeHost(m[1]), m[2]
		db := ""
		if i := strings.LastIndex(raw, "/"); i >= 0 && i+1 < len(raw) {
			db = strings.SplitN(raw[i+1:], "?", 2)[0]
		}
		return fmt.Sprintf("mysql|%s|%s|%s", host, port, db), true
	case "postgres", "mssql":
		u, err := url.Parse(raw)
		if err != nil {
			return "", false
		}
		host, port := normalizeHost(u.Hostname()), u.Port()
		db := strings.TrimPrefix(u.Path, "/")
		if scheme == "mssql" {
			db = u.Query().Get("database")
		}
		return fmt.Sprintf("%s|%s|%s|%s", scheme, host, port, db), true
	default:
		return "", false
	}
}
