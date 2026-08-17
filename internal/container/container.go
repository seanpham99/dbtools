package container

import (
	"bytes"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

const Name = "dbtools-mssql-local"

const DatabaseName = "dbtools_local"

const Image = "mcr.microsoft.com/mssql/server:2025-latest"

const Port = "14330"

const password = "Dbtools@Local123"

var escapedPassword = url.QueryEscape(password)

func connectionURL(database string) string {
	return fmt.Sprintf("mssql://sa:%s@127.0.0.1:%s?database=%s&TrustServerCertificate=true", escapedPassword, Port, database)
}

func LocalDatabaseURL() string {
	return connectionURL(DatabaseName)
}

func MasterURL() string {
	return connectionURL("master")
}

// checkDocker verifies the docker CLI is installed and its daemon is
// responding, returning an actionable error when either is missing —
// e.g. on hosts without Docker (such as Replit), where `dbtools start`
// can never work and the user should point their local target's env var
// at an external MSSQL instance instead.
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

dbtools start/stop manage a tool-owned local MSSQL container, which needs Docker.
On hosts without Docker, point your local target's connection env var (e.g.
DBTOOLS_LOCAL_URL) at an MSSQL instance you run elsewhere, then use
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

func inspect() (exists bool, running bool, err error) {
	cmd := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", Name)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	runErr := cmd.Run()
	return parseInspectOutput(out.Bytes(), runErr)
}

func Start() (string, error) {
	if err := checkDocker(); err != nil {
		return "", err
	}
	exists, running, err := inspect()
	if err != nil {
		return "", err
	}

	switch {
	case running:
	case exists:
		if out, err := exec.Command("docker", "start", Name).CombinedOutput(); err != nil {
			return "", fmt.Errorf("docker start failed: %w: %s", err, strings.TrimSpace(string(out)))
		}
	default:
		args := []string{
			"run", "-d",
			"--name", Name,
			"-e", "ACCEPT_EULA=Y",
			"-e", "MSSQL_SA_PASSWORD=" + password,
			"-p", Port + ":1433",
			Image,
		}
		if out, err := exec.Command("docker", args...).CombinedOutput(); err != nil {
			return "", fmt.Errorf("docker run failed: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}

	if err := waitReady(); err != nil {
		return "", err
	}
	if err := createDatabase(); err != nil {
		return "", err
	}
	return LocalDatabaseURL(), nil
}

func waitReady() error {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		cmd := exec.Command("docker", "exec", Name, "/opt/mssql-tools18/bin/sqlcmd", "-S", "localhost", "-U", "sa", "-P", password, "-C", "-Q", "SELECT 1")
		if err := cmd.Run(); err == nil {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("MSSQL did not become ready within 60s")
}

func createDatabase() error {
	query := fmt.Sprintf("IF DB_ID(N'%s') IS NULL CREATE DATABASE %s", DatabaseName, DatabaseName)
	cmd := exec.Command("docker", "exec", Name, "/opt/mssql-tools18/bin/sqlcmd", "-S", "localhost", "-U", "sa", "-P", password, "-C", "-Q", query)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("creating %s: %w: %s", DatabaseName, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func Stop() error {
	if err := checkDocker(); err != nil {
		return err
	}
	exists, _, err := inspect()
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if out, err := exec.Command("docker", "rm", "-f", Name).CombinedOutput(); err != nil {
		return fmt.Errorf("docker rm failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
