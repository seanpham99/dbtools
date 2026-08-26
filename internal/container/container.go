// Package container manages the tool-owned local database containers
// (one per supported engine, scoped to the current project) through the
// docker CLI.
package container

import (
	"bytes"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "github.com/microsoft/go-mssqldb"
)

// DatabaseName is the local development database every engine's container
// hosts.
const DatabaseName = "dbtools_local"

const password = "Dbtools@Local123"

var escapedPassword = url.QueryEscape(password)

// spec describes one engine's tool-owned local container template. name
// and hostPort are zero-valued here and filled in per-invocation by the
// exported functions below — two dbtools projects on one machine must
// never share a container name or a fixed host port.
type spec struct {
	engine        string
	name          string // resolved container name; empty on the package-level templates
	image         string
	containerPort string // the engine's fixed in-container port, e.g. "5432"
	hostPort      string // resolved host port; "0" means "ask Docker to assign one"
	dataDir       string // in-container path mounted to this project's data volume
	runArgs       func(s spec) []string
	// scratchRunArgs builds docker run args for an ephemeral, throwaway
	// instance used by `dbtools diff` — no volume mount (nothing to
	// persist), --rm (auto-removed on stop), never the deterministic
	// project-scoped name containerNameFor produces. nil for engines with
	// no container template (sqlite).
	scratchRunArgs func(s spec) []string
	readyProbe     func(s spec) error
	createDBArgs   func(s spec) []string // docker exec args creating DatabaseName idempotently; nil when the image does it itself
	url            func(s spec, database string) string
	// hostPortFromLocalURL extracts the host and port this engine's local
	// connection URL encodes, so MaintenanceURLFor can reuse whatever port
	// the container actually ended up on (fixed or Docker-assigned)
	// without a live docker inspect call, and can refuse to build a
	// maintenance connection for a URL that isn't the tool-owned
	// loopback container at all.
	hostPortFromLocalURL func(rawURL string) (host, port string, err error)
	maintenanceDB        string // database used for administrative connections (drop/recreate); "" means "connect with no database selected"
}

// standardURLPort extracts the host and port from an RFC 3986 URL
// (postgres, mssql — both use a plain host:port authority).
func standardURLPort(rawURL string) (host, port string, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", fmt.Errorf("parsing URL: %w", err)
	}
	if u.Port() == "" {
		return "", "", fmt.Errorf("URL %q has no port", rawURL)
	}
	return u.Hostname(), u.Port(), nil
}

var mysqlTCPRE = regexp.MustCompile(`tcp\(([^:]*):(\d+)\)`)

// mysqlURLPort extracts the host and port from MySQL's non-RFC-3986
// tcp(host:port) host syntax, which net/url.Parse cannot handle.
func mysqlURLPort(rawURL string) (host, port string, err error) {
	m := mysqlTCPRE.FindStringSubmatch(rawURL)
	if m == nil {
		return "", "", fmt.Errorf("mysql URL %q missing tcp(host:port) syntax", rawURL)
	}
	return m[1], m[2], nil
}

// isLoopbackHost reports whether host is one of the forms the tool-owned
// local containers actually bind to. MaintenanceURLFor uses this to
// refuse building a drop/recreate connection for any URL that isn't
// genuinely the tool-owned container — see reset.go's recreateLocalDatabase,
// which deliberately never aims at a real server.
func isLoopbackHost(host string) bool {
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

var mssqlSpec = spec{
	engine:        "mssql",
	image:         "mcr.microsoft.com/mssql/server:2025-latest",
	containerPort: "1433",
	dataDir:       "/var/opt/mssql",
	runArgs: func(s spec) []string {
		return []string{
			"run", "-d",
			"--name", s.name,
			"-e", "ACCEPT_EULA=Y",
			"-e", "MSSQL_SA_PASSWORD=" + password,
			"-p", s.hostPort + ":" + s.containerPort,
			"-v", volumeNameFor(s.name) + ":" + s.dataDir,
			s.image,
		}
	},
	scratchRunArgs: func(s spec) []string {
		return []string{
			"run", "-d", "--rm",
			"--name", s.name,
			"-e", "ACCEPT_EULA=Y",
			"-e", "MSSQL_SA_PASSWORD=" + password,
			"-p", s.hostPort + ":" + s.containerPort,
			s.image,
		}
	},
	readyProbe: func(s spec) error {
		u, err := url.Parse(s.url(s, "master"))
		if err == nil {
			u.Scheme = "sqlserver"
			if db, err := sql.Open("sqlserver", u.String()); err == nil {
				defer db.Close()
				if pingErr := db.Ping(); pingErr == nil {
					return nil
				}
			}
		}
		return exec.Command("docker", "exec", s.name, "/opt/mssql-tools18/bin/sqlcmd", "-S", "localhost", "-U", "sa", "-P", password, "-C", "-Q", "SELECT 1").Run()
	},
	createDBArgs: func(s spec) []string {
		query := fmt.Sprintf("IF DB_ID(N'%s') IS NULL CREATE DATABASE %s", DatabaseName, DatabaseName)
		return []string{"exec", s.name, "/opt/mssql-tools18/bin/sqlcmd", "-S", "localhost", "-U", "sa", "-P", password, "-C", "-Q", query}
	},
	url: func(s spec, database string) string {
		return fmt.Sprintf("mssql://sa:%s@127.0.0.1:%s?database=%s&TrustServerCertificate=true", escapedPassword, s.hostPort, database)
	},
	hostPortFromLocalURL: standardURLPort,
	maintenanceDB:        "master",
}

var postgresSpec = spec{
	engine:        "postgres",
	image:         "postgres:17-alpine",
	containerPort: "5432",
	dataDir:       "/var/lib/postgresql/data",
	runArgs: func(s spec) []string {
		return []string{
			"run", "-d",
			"--name", s.name,
			"-e", "POSTGRES_PASSWORD=" + password,
			"-e", "POSTGRES_DB=" + DatabaseName,
			"-p", s.hostPort + ":" + s.containerPort,
			"-v", volumeNameFor(s.name) + ":" + s.dataDir,
			s.image,
		}
	},
	scratchRunArgs: func(s spec) []string {
		return []string{
			"run", "-d", "--rm",
			"--name", s.name,
			"-e", "POSTGRES_PASSWORD=" + password,
			"-e", "POSTGRES_DB=" + DatabaseName,
			"-p", s.hostPort + ":" + s.containerPort,
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
	hostPortFromLocalURL: standardURLPort,
	maintenanceDB:        "postgres",
}

var mysqlSpec = spec{
	engine:        "mysql",
	image:         "mysql:8",
	containerPort: "3306",
	dataDir:       "/var/lib/mysql",
	runArgs: func(s spec) []string {
		return []string{
			"run", "-d",
			"--name", s.name,
			"-e", "MYSQL_ROOT_PASSWORD=" + password,
			"-e", "MYSQL_DATABASE=" + DatabaseName,
			"-p", s.hostPort + ":" + s.containerPort,
			"-v", volumeNameFor(s.name) + ":" + s.dataDir,
			s.image,
		}
	},
	scratchRunArgs: func(s spec) []string {
		return []string{
			"run", "-d", "--rm",
			"--name", s.name,
			"-e", "MYSQL_ROOT_PASSWORD=" + password,
			"-e", "MYSQL_DATABASE=" + DatabaseName,
			"-p", s.hostPort + ":" + s.containerPort,
			s.image,
		}
	},
	readyProbe: func(s spec) error {
		dsn := fmt.Sprintf("root:%s@tcp(127.0.0.1:%s)/%s?parseTime=true", password, s.hostPort, DatabaseName)
		db, err := sql.Open("mysql", dsn)
		if err != nil {
			return err
		}
		defer db.Close()
		return db.Ping()
	},
	// MYSQL_DATABASE creates DatabaseName on first boot; nothing else needed.
	createDBArgs: nil,
	url: func(s spec, database string) string {
		return fmt.Sprintf("mysql://root:%s@tcp(127.0.0.1:%s)/%s", escapedPassword, s.hostPort, database)
	},
	hostPortFromLocalURL: mysqlURLPort,
	// mysql has no separate admin database; root can DROP/CREATE
	// DatabaseName while connected with no database selected at all,
	// unlike postgres/mssql which need a *different* existing database to
	// connect to before dropping the one they're pointed at.
	maintenanceDB: "",
}

var specs = map[string]spec{
	"mssql":    mssqlSpec,
	"postgres": postgresSpec,
	"mysql":    mysqlSpec,
}

func specFor(engineName string) (spec, error) {
	s, ok := specs[engineName]
	if !ok {
		return spec{}, fmt.Errorf("no local container template for engine %q (supported: mssql, postgres, mysql)", engineName)
	}
	return s, nil
}

// containerNameFor returns the deterministic, project-scoped container
// name for engineName under projectID (see internal/projectid.Resolve).
func containerNameFor(engineName, projectID string) string {
	return fmt.Sprintf("dbtools-%s-%s", engineName, projectID)
}

// scratchNameFor returns a unique, non-deterministic name for an
// ephemeral diff-scratch container — deliberately never matching
// containerNameFor's project-scoped naming scheme, so a diff run can
// never collide with, reuse, or be confused with dbtools start's
// persistent dev container.
func scratchNameFor(engineName string) string {
	return fmt.Sprintf("dbtools-diff-scratch-%s-%d", engineName, time.Now().UnixNano())
}

// volumeNameFor returns the data volume name for a container named
// containerName.
func volumeNameFor(containerName string) string {
	return containerName + "-data"
}

// LocalDatabaseURLFor returns engineName's local database URL for a
// container already bound to hostPort. Pure — does not touch Docker or
// require a running container, useful for tests and for any caller that
// already knows the port (e.g. a pinned [container] port).
func LocalDatabaseURLFor(engineName, hostPort string) (string, error) {
	s, err := specFor(engineName)
	if err != nil {
		return "", err
	}
	s.hostPort = hostPort
	return s.url(s, DatabaseName), nil
}

// MaintenanceURLFor returns the administrative connection URL for
// engineName's tool-owned container, by swapping localURL's database for
// the engine's maintenance database and reusing localURL's already-
// resolved host and port — the container's actual published port, which
// may have been Docker-assigned rather than fixed.
func MaintenanceURLFor(engineName, localURL string) (string, error) {
	s, err := specFor(engineName)
	if err != nil {
		return "", err
	}
	host, port, err := s.hostPortFromLocalURL(localURL)
	if err != nil {
		return "", fmt.Errorf("reading host/port from local URL: %w", err)
	}
	if !isLoopbackHost(host) {
		return "", fmt.Errorf("local target %q does not point at the tool-owned container (host %q is not loopback); dbtools reset only ever targets the local container, never a remote server", localURL, host)
	}
	s.hostPort = port
	return s.url(s, s.maintenanceDB), nil
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

// discoverHostPort returns the host port Docker assigned to name's
// published containerPort/tcp mapping.
func discoverHostPort(name, containerPort string) (string, error) {
	format := fmt.Sprintf(`{{(index (index .NetworkSettings.Ports "%s/tcp") 0).HostPort}}`, containerPort)
	out, err := exec.Command("docker", "inspect", "-f", format, name).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker inspect (port lookup) failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	port := strings.TrimSpace(string(out))
	if port == "" || port == "<no value>" {
		return "", fmt.Errorf("docker did not report a host port for %s/tcp on %s", containerPort, name)
	}
	return port, nil
}

// StartForWithTimeout starts (or reuses) engineName's tool-owned local
// container scoped to projectID, waits up to timeout for readiness if
// wait is true, and returns the local database's connection URL.
// configuredPort pins the published host port; "" lets Docker assign a
// free one, discovered afterward via docker inspect.
func StartForWithTimeout(engineName, projectID, configuredPort string, timeout time.Duration, wait bool) (string, error) {
	s, err := specFor(engineName)
	if err != nil {
		return "", err
	}
	s.name = containerNameFor(engineName, projectID)
	if err := checkDocker(); err != nil {
		return "", err
	}
	exists, running, err := inspect(s.name)
	if err != nil {
		return "", err
	}

	switch {
	case running:
		port, err := discoverHostPort(s.name, s.containerPort)
		if err != nil {
			return "", err
		}
		s.hostPort = port
	case exists:
		if out, err := exec.Command("docker", "start", s.name).CombinedOutput(); err != nil {
			return "", fmt.Errorf("docker start failed: %w: %s", err, strings.TrimSpace(string(out)))
		}
		port, err := discoverHostPort(s.name, s.containerPort)
		if err != nil {
			return "", err
		}
		s.hostPort = port
	default:
		s.hostPort = configuredPort
		if s.hostPort == "" {
			s.hostPort = "0"
		}
		if out, err := exec.Command("docker", s.runArgs(s)...).CombinedOutput(); err != nil {
			return "", fmt.Errorf("docker run failed: %w: %s", err, strings.TrimSpace(string(out)))
		}
		if s.hostPort == "0" {
			port, err := discoverHostPort(s.name, s.containerPort)
			if err != nil {
				return "", err
			}
			s.hostPort = port
		}
	}

	if wait {
		if err := waitReadyWithTimeout(s, timeout); err != nil {
			return "", err
		}
	}
	if s.createDBArgs != nil {
		if out, err := exec.Command("docker", s.createDBArgs(s)...).CombinedOutput(); err != nil {
			return "", fmt.Errorf("creating %s: %w: %s", DatabaseName, err, strings.TrimSpace(string(out)))
		}
	}
	return s.url(s, DatabaseName), nil
}

func waitReadyWithTimeout(s spec, timeout time.Duration) error {
	if timeout <= 0 {
		return nil
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := s.readyProbe(s); err == nil {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("%s did not become ready within %v", s.engine, timeout)
}

// StartScratch starts a throwaway, --rm container for engineName, waits
// up to 60s for readiness, and returns its connection URL plus a cleanup
// function that stops (and thereby removes) it. Unlike StartForWithTimeout,
// there is no reuse-if-running path — every call creates a fresh
// container, since a scratch database must start empty. Returns an error
// for engines with no scratchRunArgs (sqlite — callers use a tempfile).
func StartScratch(engineName string) (rawURL string, cleanup func() error, err error) {
	s, err := specFor(engineName)
	if err != nil {
		return "", nil, err
	}
	if s.scratchRunArgs == nil {
		return "", nil, fmt.Errorf("no scratch container template for engine %q", engineName)
	}
	if err := checkDocker(); err != nil {
		return "", nil, err
	}
	s.name = scratchNameFor(engineName)
	s.hostPort = "0"

	if out, err := exec.Command("docker", s.scratchRunArgs(s)...).CombinedOutput(); err != nil {
		return "", nil, fmt.Errorf("docker run failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	cleanup = func() error {
		out, err := exec.Command("docker", "stop", s.name).CombinedOutput()
		if err != nil {
			return fmt.Errorf("docker stop failed: %w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}

	// abortWith cleans up the just-started container and folds any
	// cleanup failure into the returned error rather than dropping it —
	// a container that fails to stop needs a manual `docker rm -f`, and
	// silently losing that fact leaves it running unnoticed.
	abortWith := func(err error) error {
		if cerr := cleanup(); cerr != nil {
			return fmt.Errorf("%w (cleanup also failed: %v)", err, cerr)
		}
		return err
	}

	port, err := discoverHostPort(s.name, s.containerPort)
	if err != nil {
		return "", nil, abortWith(err)
	}
	s.hostPort = port

	if err := waitReadyWithTimeout(s, 60*time.Second); err != nil {
		return "", nil, abortWith(err)
	}
	if s.createDBArgs != nil {
		if out, err := exec.Command("docker", s.createDBArgs(s)...).CombinedOutput(); err != nil {
			return "", nil, abortWith(fmt.Errorf("creating %s: %w: %s", DatabaseName, err, strings.TrimSpace(string(out))))
		}
	}
	return s.url(s, DatabaseName), cleanup, nil
}

// StopFor stops and removes engineName's tool-owned local container
// scoped to projectID. Its data volume survives unless purgeVolume is
// true (dbtools stop --no-backup), so a later Start resumes with the
// same data.
func StopFor(engineName, projectID string, purgeVolume bool) error {
	s, err := specFor(engineName)
	if err != nil {
		return err
	}
	s.name = containerNameFor(engineName, projectID)
	if err := checkDocker(); err != nil {
		return err
	}
	exists, _, err := inspect(s.name)
	if err != nil {
		return err
	}
	if exists {
		if out, err := exec.Command("docker", "rm", "-f", s.name).CombinedOutput(); err != nil {
			return fmt.Errorf("docker rm failed: %w: %s", err, strings.TrimSpace(string(out)))
		}
	}
	if purgeVolume {
		if out, err := exec.Command("docker", "volume", "rm", volumeNameFor(s.name)).CombinedOutput(); err != nil {
			if !strings.Contains(strings.ToLower(string(out)), "no such volume") {
				return fmt.Errorf("docker volume rm failed: %w: %s", err, strings.TrimSpace(string(out)))
			}
		}
	}
	return nil
}

// RestartForWithTimeout stops then starts engineName's tool-owned local
// container scoped to projectID — its data volume survives the cycle —
// and returns the (possibly new) connection URL.
func RestartForWithTimeout(engineName, projectID, configuredPort string, timeout time.Duration, wait bool) (string, error) {
	if err := StopFor(engineName, projectID, false); err != nil {
		return "", err
	}
	return StartForWithTimeout(engineName, projectID, configuredPort, timeout, wait)
}

// LogsFor streams engineName's tool-owned local container's logs (scoped
// to projectID) straight through to stdout/stderr, following new output
// if follow is true.
func LogsFor(engineName, projectID string, follow bool) error {
	s, err := specFor(engineName)
	if err != nil {
		return err
	}
	s.name = containerNameFor(engineName, projectID)
	if err := checkDocker(); err != nil {
		return err
	}
	args := []string{"logs"}
	if follow {
		args = append(args, "-f")
	}
	args = append(args, s.name)
	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
