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
)

// StripPostgresSessionState removes the two session-state lines pg_dump
// emits by default that poison every later migration replayed through
// the same connection: a search_path reset (breaks unqualified name
// resolution) and a client_min_messages override (silently swallows
// RAISE NOTICE for the rest of the session). See the design spec's
// "Native schema dump" section.
func StripPostgresSessionState(sqlText string) string {
	sqlText = pgSetConfigSearchPathRE.ReplaceAllString(sqlText, "")
	sqlText = pgClientMinMessagesRE.ReplaceAllString(sqlText, "")
	return sqlText
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
