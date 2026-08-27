package dump

import (
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"regexp"
	"strings"

	"github.com/go-sql-driver/mysql"
	"github.com/seanpham99/dbtools/internal/container"
	"github.com/seanpham99/dbtools/internal/engine"
)

// Options controls where Schema runs the engine's dump tool.
type Options struct {
	// ExecIn names a Docker container hosting the database being dumped.
	// When set, and the engine's image ships the tool, the dump runs inside
	// that container so the tool's version always matches the server's.
	// Empty means "run the tool on the host".
	ExecIn string
	// UseHostTools forces the host binary even when ExecIn is available —
	// the escape hatch for environments without Docker, or operators who
	// know their client and server versions agree.
	UseHostTools bool
}

// Schema returns the DDL that reproduces eng's schema at scratchURL, via the
// engine's native dump tool (pg_dump/mysqldump/mssql-scripter) or, for
// SQLite, a direct catalog query — no external tool needed. Any tables in
// excludeTables (e.g. migration tracking tables) are omitted.
//
// The tool runs inside opts.ExecIn when it can (see Options), because a dump
// tool newer than the server it dumps emits statements that server cannot
// execute — pg_dump 17+ writes `SET transaction_timeout`, which Postgres 16
// rejects. The output is committed as a baseline, so a mismatch does not stay
// contained: it resurfaces whenever that baseline is applied.
func Schema(eng engine.Engine, scratchURL string, opts Options, excludeTables ...string) (string, error) {
	switch eng.Name() {
	case "postgres":
		return dumpPostgres(scratchURL, opts, excludeTables)
	case "mysql":
		return dumpMySQL(scratchURL, opts, excludeTables)
	case "mssql":
		// mssql-scripter is a separate Python package, not part of the
		// SQL Server image, so this one always runs on the host.
		return dumpMSSQL(scratchURL, excludeTables)
	case "sqlite":
		db, err := eng.Open(scratchURL)
		if err != nil {
			return "", err
		}
		defer db.Close()
		return SchemaFromDB(eng, db, excludeTables...)
	default:
		return "", fmt.Errorf("no schema dump support for engine %q", eng.Name())
	}
}

func dumpPostgres(scratchURL string, opts Options, excludeTables []string) (string, error) {
	const toolName = "pg_dump"
	args := []string{"--no-owner", "--schema-only"}
	for _, tbl := range excludeTables {
		if tbl != "" {
			args = append(args, "-T", tbl, "-T", "*."+tbl)
		}
	}
	connURL, inContainer, err := dumpTarget("postgres", scratchURL, opts)
	if err != nil {
		return "", err
	}
	out, err := runDumpTool(toolName, append(args, connURL), opts, inContainer,
		"install postgresql-client to use squash with postgres")
	if err != nil {
		return "", err
	}
	return StripPostgresSessionState(out), nil
}

// dumpTarget decides where the dump tool will run and which connection URL
// it should use. Inside the container the engine listens on its own fixed
// port, so the host port mapping does not apply.
func dumpTarget(engineName, hostURL string, opts Options) (connURL string, inContainer bool, err error) {
	if opts.ExecIn == "" || opts.UseHostTools || !container.SupportsInContainerDump(engineName) {
		return hostURL, false, nil
	}
	inURL, err := container.InContainerURL(engineName)
	if err != nil {
		return "", false, err
	}
	return inURL, true, nil
}

// runDumpTool executes toolName with args, either inside opts.ExecIn or on
// the host. args must already carry the connection details — the engines
// differ on whether that is a trailing URL or a set of flags. hostHint tells
// the user how to install the tool when the host path is taken and it is
// missing.
func runDumpTool(toolName string, args []string, opts Options, inContainer bool, hostHint string) (string, error) {
	if inContainer {
		out, err := container.Exec(opts.ExecIn, append([]string{toolName}, args...)...)
		if err != nil {
			return "", err
		}
		return string(out), nil
	}
	if _, err := exec.LookPath(toolName); err != nil {
		return "", fmt.Errorf("%s not found on PATH — %s: %w", toolName, hostHint, err)
	}
	out, err := exec.Command(toolName, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s failed: %w: %s", toolName, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func dumpMySQL(scratchURL string, opts Options, excludeTables []string) (string, error) {
	const toolName = "mysqldump"
	connURL, inContainer, err := dumpTarget("mysql", scratchURL, opts)
	if err != nil {
		return "", err
	}

	args := []string{"--no-tablespaces", "--skip-comments", "--no-data"}
	raw := strings.TrimPrefix(connURL, "mysql://")
	cfg, parseErr := mysql.ParseDSN(raw)
	if parseErr == nil && cfg != nil {
		if cfg.Addr != "" {
			host, port, splitErr := net.SplitHostPort(cfg.Addr)
			if splitErr == nil {
				args = append(args, "-h", host, "-P", port)
			} else {
				args = append(args, "-h", cfg.Addr)
			}
		}
		if cfg.User != "" {
			args = append(args, "-u", cfg.User)
		}
		if cfg.Passwd != "" {
			args = append(args, "--password="+cfg.Passwd)
		}
		for _, tbl := range excludeTables {
			if tbl != "" {
				if cfg.DBName != "" {
					args = append(args, fmt.Sprintf("--ignore-table=%s.%s", cfg.DBName, tbl))
				} else {
					args = append(args, fmt.Sprintf("--ignore-table=%s", tbl))
				}
			}
		}
		if cfg.DBName != "" {
			args = append(args, cfg.DBName)
		}
	} else {
		args = append(args, connURL)
	}

	return runDumpTool(toolName, args, opts, inContainer,
		"install mysql-client or mariadb-client to use squash with mysql")
}

func dumpMSSQL(scratchURL string, excludeTables []string) (string, error) {
	const toolName = "mssql-scripter"
	if _, err := exec.LookPath(toolName); err != nil {
		return "", fmt.Errorf("%s not found on PATH — install it via 'pip install mssql-scripter' to use squash with mssql: %w", toolName, err)
	}

	var args []string
	u, err := url.Parse(scratchURL)
	if err == nil && u != nil {
		server := u.Hostname()
		if port := u.Port(); port != "" {
			server = server + "," + port
		}
		if server != "" {
			args = append(args, "-S", server)
		}
		if u.User != nil {
			if user := u.User.Username(); user != "" {
				args = append(args, "-U", user)
			}
			if pass, ok := u.User.Password(); ok && pass != "" {
				args = append(args, "-P", pass)
			}
		}
		dbName := u.Query().Get("database")
		if dbName == "" {
			dbName = strings.TrimPrefix(u.Path, "/")
		}
		if dbName != "" {
			args = append(args, "-d", dbName)
		}
	} else {
		args = append(args, scratchURL)
	}

	if len(excludeTables) > 0 {
		var validExcludes []string
		for _, tbl := range excludeTables {
			if tbl != "" {
				validExcludes = append(validExcludes, tbl)
			}
		}
		if len(validExcludes) > 0 {
			args = append(args, "--exclude-objects")
			args = append(args, validExcludes...)
		}
	}

	out, err := exec.Command(toolName, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s failed: %w: %s", toolName, err, strings.TrimSpace(string(out)))
	}
	return StripMSSQLUseStatement(string(out)), nil
}

var (
	pgSetConfigSearchPathRE = regexp.MustCompile(`(?m)^SELECT pg_catalog\.set_config\('search_path', '', false\);\n?`)
	pgClientMinMessagesRE   = regexp.MustCompile(`(?m)^SET client_min_messages = warning;\n?`)
	mssqlUseRE              = regexp.MustCompile(`(?im)^\s*USE\s+\[?[^;\n]+\]?\s*;?\s*(\r?\n)?`)

	// pg_dump's preamble sets four timeout GUCs. They tune the restore
	// session and say nothing about the schema, but they are not all
	// present in every server version — transaction_timeout arrived in
	// Postgres 17, so a dump taken with pg_dump 17+ fails to apply to a
	// 16 server with `unrecognized configuration parameter`.
	//
	// Running the dump tool inside the server's own container removes the
	// mismatch at the source, so this is now only reachable on the
	// --use-host-tools path, where the operator has taken responsibility
	// for matching versions. Kept because that path still exists and
	// because stripping restore-tuning settings costs nothing.
	//
	// Deliberately narrow: the other preamble SETs are kept, because some
	// change behaviour that matters to a restore. check_function_bodies =
	// false in particular is what lets a function referencing a
	// not-yet-created object be defined at all.
	pgTimeoutGUCRE = regexp.MustCompile(`(?m)^SET (statement_timeout|lock_timeout|idle_in_transaction_session_timeout|transaction_timeout) = [^;\n]*;\n?`)
)

// StripPostgresSessionState removes the pg_dump output that a baseline
// applied over the wire protocol cannot survive:
//
//   - a search_path reset (breaks unqualified name resolution for every
//     later migration replayed through the same connection),
//   - a client_min_messages override (silently swallows RAISE NOTICE for
//     the rest of the session),
//   - psql meta-commands (see StripPsqlMetaCommands),
//   - the timeout GUCs, which are not present in every server version.
//
// See the design spec's "Native schema dump" section.
func StripPostgresSessionState(sqlText string) string {
	sqlText = pgSetConfigSearchPathRE.ReplaceAllString(sqlText, "")
	sqlText = pgClientMinMessagesRE.ReplaceAllString(sqlText, "")
	sqlText = pgTimeoutGUCRE.ReplaceAllString(sqlText, "")
	sqlText = StripPsqlMetaCommands(sqlText)
	return sqlText
}

// StripPsqlMetaCommands removes backslash meta-commands from dump output.
//
// These are psql client directives, not SQL: the wire protocol rejects them
// outright ("syntax error at or near \"\\\""). pg_dump emits \restrict and
// \unrestrict by default on 16.10+, 17.6+ and 18.x — a security hardening
// measure — so on any current point release an unstripped baseline fails to
// apply, both to squash's own verification database and later to any target
// replaying the committed file.
//
// Only lines whose first non-whitespace character is a backslash are dropped,
// and only outside dollar-quoted bodies: a function body is free to contain
// backslash-led lines that are ordinary text, and removing those would
// silently corrupt the routine rather than fail loudly.
func StripPsqlMetaCommands(sqlText string) string {
	if !strings.Contains(sqlText, `\`) {
		return sqlText
	}
	lines := strings.Split(sqlText, "\n")
	kept := make([]string, 0, len(lines))
	dollarTag := "" // non-empty while inside a dollar-quoted body
	for _, line := range lines {
		if dollarTag == "" && strings.HasPrefix(strings.TrimLeft(line, " \t"), `\`) {
			continue
		}
		dollarTag = trackDollarQuote(line, dollarTag)
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// trackDollarQuote returns the dollar-quote tag still open at the end of
// line, given the tag open at its start ("" for none). Scanning is
// tag-aware: a body opened by $func$ ends only at $func$, so a bare $$
// inside it does not close it.
func trackDollarQuote(line, open string) string {
	for i := 0; i < len(line); {
		if open != "" {
			idx := strings.Index(line[i:], open)
			if idx < 0 {
				return open
			}
			i += idx + len(open)
			open = ""
			continue
		}
		idx := strings.IndexByte(line[i:], '$')
		if idx < 0 {
			return ""
		}
		i += idx
		tag := dollarTagAt(line[i:])
		if tag == "" {
			i++
			continue
		}
		open = tag
		i += len(tag)
	}
	return open
}

// dollarTagAt returns the dollar-quote tag starting at s ("$$" or "$name$"),
// or "" if s does not start one. Tag bodies follow Postgres's identifier
// rules: letters, underscores, and digits after the first character.
func dollarTagAt(s string) string {
	if len(s) == 0 || s[0] != '$' {
		return ""
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c == '$' {
			return s[:i+1]
		}
		isIdent := c == '_' ||
			(c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9' && i > 1)
		if !isIdent {
			return ""
		}
	}
	return ""
}

// StripMSSQLUseStatement removes any generated USE [database] commands
// so the baseline script is not pinned to the throwaway scratch database name.
func StripMSSQLUseStatement(sqlText string) string {
	return mssqlUseRE.ReplaceAllString(sqlText, "")
}

// SchemaFromDB queries db's own catalog directly — only implemented for
// SQLite today (sqlite_master.sql already contains verbatim CREATE
// statements; no external dump tool exists or is needed).
func SchemaFromDB(eng engine.Engine, db *sql.DB, excludeTables ...string) (string, error) {
	if eng.Name() != "sqlite" {
		return "", fmt.Errorf("SchemaFromDB is only implemented for sqlite, got %q", eng.Name())
	}
	rows, err := db.Query(`SELECT tbl_name, sql FROM sqlite_master WHERE sql IS NOT NULL AND type IN ('table', 'index', 'view', 'trigger') ORDER BY rowid`)
	if err != nil {
		return "", fmt.Errorf("querying sqlite_master: %w", err)
	}
	defer rows.Close()

	excludeMap := make(map[string]bool)
	for _, tbl := range excludeTables {
		if tbl != "" {
			excludeMap[tbl] = true
		}
	}

	var b strings.Builder
	for rows.Next() {
		var tblName, stmt string
		if err := rows.Scan(&tblName, &stmt); err != nil {
			return "", fmt.Errorf("scanning sqlite_master row: %w", err)
		}
		if excludeMap[tblName] {
			continue
		}
		b.WriteString(stmt)
		b.WriteString(";\n")
	}
	return b.String(), rows.Err()
}
