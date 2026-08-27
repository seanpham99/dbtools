package container

import (
	"fmt"
	"os/exec"
	"strings"
)

// InContainerURL returns the connection URL for DatabaseName as seen from
// *inside* the container itself, where the engine listens on its fixed port
// and the host port mapping is irrelevant.
//
// It exists so a dump tool can run inside the container that hosts the
// database it is dumping. That guarantees the tool's version matches the
// server's — a client newer than the server emits statements the server
// cannot execute (`SET transaction_timeout` from pg_dump 17+ against a 16
// server), and the resulting baseline is committed, so the mismatch would
// resurface at apply time long after the dump.
func InContainerURL(engineName string) (string, error) {
	s, err := specFor(engineName)
	if err != nil {
		return "", err
	}
	// Reuse the engine's own URL template with the in-container port, so
	// there is one place that knows each engine's URL shape.
	s.hostPort = s.containerPort
	return s.url(s, DatabaseName), nil
}

// SupportsInContainerDump reports whether engineName's image ships the dump
// tool dbtools needs.
//
// Postgres and MySQL images carry pg_dump and mysqldump. SQL Server's does
// not ship mssql-scripter — it is a separate Python package — so MSSQL dumps
// still run on the host, and version skew there remains the operator's to
// manage.
func SupportsInContainerDump(engineName string) bool {
	switch engineName {
	case "postgres", "mysql":
		return true
	default:
		return false
	}
}

// Exec runs argv inside containerName via `docker exec` and returns its
// combined output. Used to run an engine's own dump tool at the engine's own
// version.
func Exec(containerName string, argv ...string) ([]byte, error) {
	if containerName == "" {
		return nil, fmt.Errorf("no container to exec in")
	}
	args := append([]string{"exec", containerName}, argv...)
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		tool := ""
		if len(argv) > 0 {
			tool = argv[0] + " "
		}
		return nil, fmt.Errorf("%sin container %s failed: %w: %s",
			tool, containerName, err, strings.TrimSpace(string(out)))
	}
	return out, nil
}
