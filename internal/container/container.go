// Package container manages the tool-owned local database containers
// (one per supported engine) through the docker CLI.
package container

import (
	"bytes"
	"database/sql"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// DatabaseName is the local development database every engine's container
// hosts.
const DatabaseName = "dbtools_local"

const password = "Dbtools@Local123"

var escapedPassword = url.QueryEscape(password)

// spec describes one engine's tool-owned local container.
type spec struct {
	engine        string
	name          string // container name
	image         string
	hostPort      string
	runArgs       func(s spec) []string // docker run args
	readyProbe    func(s spec) error    // nil error when the server is ready
	createDBArgs  func(s spec) []string // docker exec args creating DatabaseName idempotently; nil when the image does it itself
	url           func(s spec, database string) string
	maintenanceDB string // database used for administrative connections (drop/recreate)
}

var mssqlSpec = spec{
	engine:   "mssql",
	name:     "dbtools-mssql-local",
	image:    "mcr.microsoft.com/mssql/server:2025-latest",
	hostPort: "14330",
	runArgs: func(s spec) []string {
		return []string{
			"run", "-d",
			"--name", s.name,
			"-e", "ACCEPT_EULA=Y",
			"-e", "MSSQL_SA_PASSWORD=" + password,
			"-p", s.hostPort + ":1433",
			s.image,
		}
	},
	readyProbe: func(s spec) error {
		return exec.Command("docker", "exec", s.name, "/opt/mssql-tools18/bin/sqlcmd", "-S", "localhost", "-U", "sa", "-P", password, "-C", "-Q", "SELECT 1").Run()
	},
	createDBArgs: func(s spec) []string {
		query := fmt.Sprintf("IF DB_ID(N'%s') IS NULL CREATE DATABASE %s", DatabaseName, DatabaseName)
		return []string{"exec", s.name, "/opt/mssql-tools18/bin/sqlcmd", "-S", "localhost", "-U", "sa", "-P", password, "-C", "-Q", query}
	},
	url: func(s spec, database string) string {
		return fmt.Sprintf("mssql://sa:%s@127.0.0.1:%s?database=%s&TrustServerCertificate=true", escapedPassword, s.hostPort, database)
	},
	maintenanceDB: "master",
}

var postgresSpec = spec{
	engine:   "postgres",
	name:     "dbtools-postgres-local",
	image:    "postgres:17-alpine",
	hostPort: "54320",
	runArgs: func(s spec) []string {
		return []string{
			"run", "-d",
			"--name", s.name,
			"-e", "POSTGRES_PASSWORD=" + password,
			"-e", "POSTGRES_DB=" + DatabaseName,
			"-p", s.hostPort + ":5432",
			s.image,
		}
	},
	// Probed from the host through the published port (not docker exec,
	// which is unreliable on some container hosts): a successful Ping
	// also proves the port mapping works, which is what callers use.
	readyProbe: func(s spec) error {
		db, err := sql.Open("postgres", s.url(s, DatabaseName))
		if err != nil {
			return err
		}
		defer db.Close()
		return db.Ping()
	},
	// POSTGRES_DB creates DatabaseName on first boot; nothing else needed.
	createDBArgs: nil,
	url: func(s spec, database string) string {
		return fmt.Sprintf("postgres://postgres:%s@127.0.0.1:%s/%s?sslmode=disable", escapedPassword, s.hostPort, database)
	},
	maintenanceDB: "postgres",
}

var specs = map[string]spec{
	"mssql":    mssqlSpec,
	"postgres": postgresSpec,
}

func specFor(engineName string) (spec, error) {
	s, ok := specs[engineName]
	if !ok {
		return spec{}, fmt.Errorf("no local container template for engine %q (supported: mssql, postgres)", engineName)
	}
	return s, nil
}

// LocalDatabaseURLFor returns the connection URL for engineName's
// tool-owned local database.
func LocalDatabaseURLFor(engineName string) (string, error) {
	s, err := specFor(engineName)
	if err != nil {
		return "", err
	}
	return s.url(s, DatabaseName), nil
}

// MaintenanceURLFor returns the connection URL for engineName's
// administrative database on the tool-owned container (used by reset to
// drop/recreate the local database from outside it).
func MaintenanceURLFor(engineName string) (string, error) {
	s, err := specFor(engineName)
	if err != nil {
		return "", err
	}
	return s.url(s, s.maintenanceDB), nil
}

// MasterURL returns the MSSQL container's master-database URL. Kept for
// callers that are inherently MSSQL-scoped.
func MasterURL() string {
	return mssqlSpec.url(mssqlSpec, mssqlSpec.maintenanceDB)
}

// checkDocker verifies the docker CLI is installed and its daemon is
// responding, returning an actionable error when either is missing —
// e.g. on hosts without Docker (such as Replit), where `dbtools start`
// can never work and the user should point their local target's env var
// at an external database instance instead.
func checkDocker() error {
	if _, err := exec.LookPath("docker"); err != nil {
		return dockerUnavailableError("the docker CLI is not installed")
	}
	if out, err := exec.Command("docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput(); err != nil {
		return dockerUnavailableError("the Docker daemon is not responding: " + strings.TrimSpace(string(out)))
	}
	return nil
}

func dockerUnavailableError(cause string) error {
	return fmt.Errorf(`Docker is not available here (%s).

dbtools start/stop manage a tool-owned local database container, which needs
Docker. On hosts without Docker, point your local target's connection env var
(e.g. DBTOOLS_LOCAL_URL) at a database instance you run elsewhere, then use
'dbtools up' / 'dbtools status' directly — no container needed`, cause)
}

func parseInspectOutput(out []byte, cmdErr error) (exists bool, running bool, err error) {
	if cmdErr != nil {
		// Docker CLI message wording for a missing container has changed
		// across versions ("Error: No such container: X" pre-25, "error:
		// no such object: X" 29+) — match case-insensitively on "no such"
		// rather than the exact phrase.
		if strings.Contains(strings.ToLower(string(out)), "no such") {
			return false, false, nil
		}
		return false, false, fmt.Errorf("docker inspect failed: %w: %s", cmdErr, strings.TrimSpace(string(out)))
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "true" {
		return true, true, nil
	}
	if trimmed == "false" {
		return true, false, nil
	}
	return true, false, fmt.Errorf("unexpected docker inspect output: %q", trimmed)
}

func inspect(containerName string) (exists bool, running bool, err error) {
	cmd := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", containerName)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	runErr := cmd.Run()
	return parseInspectOutput(out.Bytes(), runErr)
}

// StartFor starts (or reuses) engineName's tool-owned local container and
// returns the local database's connection URL.
func StartFor(engineName string) (string, error) {
	s, err := specFor(engineName)
	if err != nil {
		return "", err
	}
	if err := checkDocker(); err != nil {
		return "", err
	}
	exists, running, err := inspect(s.name)
	if err != nil {
		return "", err
	}

	switch {
	case running:
	case exists:
		if out, err := exec.Command("docker", "start", s.name).CombinedOutput(); err != nil {
			return "", fmt.Errorf("docker start failed: %w: %s", err, strings.TrimSpace(string(out)))
		}
	default:
		if out, err := exec.Command("docker", s.runArgs(s)...).CombinedOutput(); err != nil {
			return "", fmt.Errorf("docker run failed: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}

	if err := waitReady(s); err != nil {
		return "", err
	}
	if s.createDBArgs != nil {
		if out, err := exec.Command("docker", s.createDBArgs(s)...).CombinedOutput(); err != nil {
			return "", fmt.Errorf("creating %s: %w: %s", DatabaseName, err, strings.TrimSpace(string(out)))
		}
	}
	return s.url(s, DatabaseName), nil
}

func waitReady(s spec) error {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if err := s.readyProbe(s); err == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("%s did not become ready within 60s", s.engine)
}

// StopFor stops and removes engineName's tool-owned local container.
func StopFor(engineName string) error {
	s, err := specFor(engineName)
	if err != nil {
		return err
	}
	if err := checkDocker(); err != nil {
		return err
	}
	exists, _, err := inspect(s.name)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if out, err := exec.Command("docker", "rm", "-f", s.name).CombinedOutput(); err != nil {
		return fmt.Errorf("docker rm failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Start starts the MSSQL container. Kept as the historical entry point;
// engine-aware callers use StartFor.
func Start() (string, error) { return StartFor("mssql") }

// Stop removes the MSSQL container. Engine-aware callers use StopFor.
func Stop() error { return StopFor("mssql") }
