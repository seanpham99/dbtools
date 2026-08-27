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
	"github.com/seanpham99/dbtools/internal/engine"
)

// Schema returns the DDL that reproduces eng's schema at scratchURL, via
// the engine's native dump tool (pg_dump/mysqldump/mssql-scripter on the
// host, connecting to scratchURL's published port) or, for SQLite, a
// direct catalog query — no external tool needed. Any tables in excludeTables
// (e.g. migration tracking tables) are omitted from the dump.
func Schema(eng engine.Engine, scratchURL string, excludeTables ...string) (string, error) {
	switch eng.Name() {
	case "postgres":
		return dumpPostgres(scratchURL, excludeTables)
	case "mysql":
		return dumpMySQL(scratchURL, excludeTables)
	case "mssql":
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

func dumpPostgres(scratchURL string, excludeTables []string) (string, error) {
	const toolName = "pg_dump"
	if _, err := exec.LookPath(toolName); err != nil {
		return "", fmt.Errorf("%s not found on PATH — install postgresql-client to use squash with postgres: %w", toolName, err)
	}
	args := []string{"--no-owner", "--schema-only"}
	for _, tbl := range excludeTables {
		if tbl != "" {
			args = append(args, "-T", tbl, "-T", "*."+tbl)
		}
	}
	args = append(args, scratchURL)
	out, err := exec.Command(toolName, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s failed: %w: %s", toolName, err, strings.TrimSpace(string(out)))
	}
	return StripPostgresSessionState(string(out)), nil
}

func dumpMySQL(scratchURL string, excludeTables []string) (string, error) {
	const toolName = "mysqldump"
	if _, err := exec.LookPath(toolName); err != nil {
		return "", fmt.Errorf("%s not found on PATH — install mysql-client or mariadb-client to use squash with mysql: %w", toolName, err)
	}

	args := []string{"--no-tablespaces", "--skip-comments", "--no-data"}
	raw := strings.TrimPrefix(scratchURL, "mysql://")
	cfg, err := mysql.ParseDSN(raw)
	if err == nil && cfg != nil {
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
		args = append(args, scratchURL)
	}

	out, err := exec.Command(toolName, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s failed: %w: %s", toolName, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
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
	// 16 server with `unrecognized configuration parameter`. pg_dump is
	// explicitly supported dumping older servers, so this mismatch is a
	// normal setup (a newer client package on the developer's machine),
	// not a misconfiguration.
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
