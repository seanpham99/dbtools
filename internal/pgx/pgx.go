// Package pgx drives the external pgx CLI — a local, disposable,
// PostgreSQL-compatible runtime — as a backend for the tool-owned local
// database.
//
// dbtools never links or redistributes pgx. pgx ships under the GNU
// Affero General Public License and dbtools under the MIT license, so the
// pgx binary stays an external tool that the user installs. dbtools
// invokes it as a separate process, the same arrangement dbtools already
// has with docker, psql, pg_dump and mssql-scripter.
//
// The cluster lives under a directory of its own (PGX_HOME) and the unix
// socket in a second one (PGX_RUN_DIR). Both are scoped by engine and by
// project, so two dbtools projects on one machine never share a cluster.
package pgx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BinaryName is the executable dbtools looks for on PATH.
const BinaryName = "pgx"

// InstallHint names the install command for a missing binary.
const InstallHint = "the pgx CLI is not installed — install it with: curl -fsSL https://pgx.dev/install | sh"

// Exit codes that pgx uses for its own failures. dbtools reads them to
// tell a cluster that does not exist yet apart from a broken runtime.
const (
	// exitNotReady means pgx is not initialised or not running.
	exitNotReady = 3
	// exitConflict means the requested state already exists.
	exitConflict = 4
)

// RootEnv names the environment variable that relocates the directory
// holding every pgx cluster. It exists so a test or a CI job can keep its
// clusters out of the user's home directory.
const RootEnv = "DBTOOLS_PGX_ROOT"

// Runner executes one command and returns its output. A test replaces it
// to drive the client without a pgx installation.
type Runner func(ctx context.Context, name string, env []string, args ...string) (stdout []byte, stderr []byte, err error)

// ExitError reports a pgx command that exited with a non-zero status.
type ExitError struct {
	// Code is the exit status pgx returned.
	Code int
	// Stderr is the diagnostic output pgx wrote.
	Stderr string
	// Err is the underlying process error.
	Err error
}

func (e *ExitError) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		msg = "pgx wrote no diagnostic output"
	}
	return fmt.Sprintf("pgx exited with status %d: %s", e.Code, msg)
}

func (e *ExitError) Unwrap() error { return e.Err }

// Client runs pgx against one cluster directory.
type Client struct {
	// Home is the PGX_HOME directory that holds the cluster.
	Home string
	// RunDir is the PGX_RUN_DIR directory that holds the unix socket.
	RunDir string
	// Run executes the binary. It defaults to execRunner.
	Run Runner
}

// Root returns the directory that holds every pgx cluster.
func Root() (string, error) {
	if root := os.Getenv(RootEnv); root != "" {
		return root, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locating the home directory: %w", err)
	}
	return filepath.Join(home, ".dbtools", "pgx"), nil
}

// Paths returns the cluster directory and the socket directory of one
// project. The socket directory is separate from the cluster because pgx
// needs a short path for the socket file.
func Paths(engineName, projectID string) (home string, runDir string, err error) {
	root, err := Root()
	if err != nil {
		return "", "", err
	}
	return filepath.Join(root, engineName, projectID),
		filepath.Join(root, "run", engineName, projectID),
		nil
}

// NewClient returns a client for one project's cluster.
func NewClient(engineName, projectID string) (*Client, error) {
	home, runDir, err := Paths(engineName, projectID)
	if err != nil {
		return nil, err
	}
	return &Client{Home: home, RunDir: runDir}, nil
}

// Available reports whether the pgx binary is on PATH.
func Available() bool {
	_, err := exec.LookPath(BinaryName)
	return err == nil
}

// MissingBinaryError describes a pgx binary that is not installed.
func MissingBinaryError() error {
	return errors.New(InstallHint)
}

// env returns the environment every pgx invocation runs with. PGX_HOME
// selects the cluster and PGX_RUN_DIR selects the socket directory.
func (c *Client) env() []string {
	return []string{
		"PGX_HOME=" + c.Home,
		"PGX_RUN_DIR=" + c.RunDir,
	}
}

// run executes one pgx command and converts a non-zero exit into an
// ExitError that carries the status code.
//
// A Runner that already knows the status may return an *ExitError
// directly; one that does not may return the error exec.Command
// produces, and the status is read from it.
func (c *Client) run(ctx context.Context, args ...string) (stdout []byte, stderr []byte, err error) {
	runner := c.Run
	if runner == nil {
		runner = execRunner
	}
	stdout, stderr, err = runner(ctx, BinaryName, c.env(), args...)
	if err == nil {
		return stdout, stderr, nil
	}
	var known *ExitError
	if errors.As(err, &known) {
		if known.Stderr == "" {
			known.Stderr = string(stderr)
		}
		return stdout, stderr, known
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout, stderr, &ExitError{Code: exitErr.ExitCode(), Stderr: string(stderr), Err: err}
	}
	return stdout, stderr, err
}

// execRunner is the default Runner. It runs the binary with the given
// environment added to the current one.
func execRunner(ctx context.Context, name string, env []string, args ...string) ([]byte, []byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.Bytes(), stderr.Bytes(), err
}

// EnsureCluster creates the cluster when it does not exist yet, without
// starting it.
//
// pgx reports an already-initialised cluster as a conflict, which is not
// an error here: the cluster is what the caller wanted.
//
// port pins the port of a cluster that is created now. It has no effect
// on a cluster that already exists, because pgx records the port at
// initialisation.
func (c *Client) EnsureCluster(ctx context.Context, port string) error {
	if err := c.ensureClusterDir(); err != nil {
		return err
	}
	_, initStderr, initErr := c.run(ctx, c.initArgs(port)...)
	if initErr == nil {
		return nil
	}
	var exitErr *ExitError
	if errors.As(initErr, &exitErr) && exitErr.Code == exitConflict {
		return nil
	}
	return fmt.Errorf("creating the pgx cluster: %w: %s", initErr, strings.TrimSpace(string(initStderr)))
}

// EnsureRunning starts the runtime, creating the cluster first when it
// does not exist yet.
//
// pgx reports a missing cluster and a stopped cluster with the same exit
// code, but the two states are distinguishable by what happens next: a
// stopped cluster starts, and a missing one does not.
//
// port pins the port of a cluster that is created now. It has no effect
// on a cluster that already exists, because pgx records the port at
// initialisation.
func (c *Client) EnsureRunning(ctx context.Context, port string) error {
	if err := c.ensureClusterDir(); err != nil {
		return err
	}
	if _, _, err := c.run(ctx, "start"); err == nil {
		return nil
	} else if !c.isNotReady(err) {
		return fmt.Errorf("starting the pgx runtime: %w", err)
	}
	if err := c.EnsureCluster(ctx, port); err != nil {
		return err
	}
	if _, _, err := c.run(ctx, "start"); err != nil {
		return fmt.Errorf("starting the pgx runtime: %w", err)
	}
	return nil
}

// initArgs returns the arguments that create the cluster. The durability
// is full because the docker provider keeps the local database's data in
// a named volume that survives an operating system crash, and the pgx
// cluster should keep the same promise.
func (c *Client) initArgs(port string) []string {
	args := []string{"init", "--durability", "full"}
	if port != "" {
		args = append(args, "--port", port)
	}
	return args
}

// isNotReady reports whether err is pgx's "not initialised or not
// running" failure.
func (c *Client) isNotReady(err error) bool {
	var exitErr *ExitError
	return errors.As(err, &exitErr) && exitErr.Code == exitNotReady
}

// ensureClusterDir creates the cluster directory. pgx creates it too, but
// only when its parent already exists.
func (c *Client) ensureClusterDir() error {
	if err := os.MkdirAll(c.Home, 0o700); err != nil {
		return fmt.Errorf("creating the pgx cluster directory %s: %w", c.Home, err)
	}
	return nil
}

// Stop shuts the runtime down. A runtime that is not running is not an
// error, because stopping something that is already stopped is what the
// caller asked for.
func (c *Client) Stop(ctx context.Context) error {
	_, stderr, err := c.run(ctx, "stop")
	if err == nil {
		return nil
	}
	if c.isNotReady(err) {
		return nil
	}
	return fmt.Errorf("stopping the pgx runtime: %w: %s", err, strings.TrimSpace(string(stderr)))
}

// Restart stops and starts the runtime, which keeps the cluster's data.
//
// A runtime that is not running is not an error: pgx restarts a stopped
// cluster as readily as a running one, so the only failure worth
// reporting is one that is neither.
func (c *Client) Restart(ctx context.Context) error {
	_, stderr, err := c.run(ctx, "restart")
	if err == nil {
		return nil
	}
	if c.isNotReady(err) {
		return fmt.Errorf("restarting the pgx runtime: %w: %s", err, strings.TrimSpace(string(stderr)))
	}
	return fmt.Errorf("restarting the pgx runtime: %w", err)
}

// EnsureDatabase creates the local database when it does not exist yet.
func (c *Client) EnsureDatabase(ctx context.Context, name string) error {
	_, stderr, err := c.run(ctx, "create", name)
	if err == nil {
		return nil
	}
	var exitErr *ExitError
	if errors.As(err, &exitErr) && exitErr.Code == exitConflict {
		return nil
	}
	return fmt.Errorf("creating the pgx database %s: %w: %s", name, err, strings.TrimSpace(string(stderr)))
}

// DatabaseURL returns the connection URL of one database in the cluster.
//
// pgx speaks no TLS, so the URL carries sslmode=disable. A driver that
// defaults to requiring TLS refuses the connection otherwise.
func (c *Client) DatabaseURL(ctx context.Context, name string) (string, error) {
	stdout, stderr, err := c.run(ctx, "--json", "url", name)
	if err != nil {
		return "", fmt.Errorf("reading the pgx URL of %s: %w: %s", name, err, strings.TrimSpace(string(stderr)))
	}
	var payload struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(stdout, &payload); err != nil {
		return "", fmt.Errorf("parsing the pgx URL of %s: %w", name, err)
	}
	if payload.URL == "" {
		return "", fmt.Errorf("pgx returned no URL for database %s", name)
	}
	return withSSLDisabled(payload.URL)
}

// withSSLDisabled adds sslmode=disable to a pgx URL.
func withSSLDisabled(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parsing the pgx URL: %w", err)
	}
	if !IsLoopbackHost(parsed.Hostname()) {
		return "", fmt.Errorf("the pgx URL points at %s, which is not a loopback address", parsed.Hostname())
	}
	query := parsed.Query()
	query.Set("sslmode", "disable")
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

// IsLoopbackHost reports whether host names the local machine.
func IsLoopbackHost(host string) bool {
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

// LogPath returns the file that pgx writes its server log to.
func (c *Client) LogPath() string {
	return filepath.Join(c.Home, "log", "pgx.log")
}

// WriteOwner records the dbtools.toml path that owns the cluster, so that
// a later prune can tell a cluster whose project directory is gone from
// one that is still in use.
func (c *Client) WriteOwner(configPath string) error {
	if err := os.MkdirAll(c.Home, 0o700); err != nil {
		return fmt.Errorf("creating the pgx cluster directory %s: %w", c.Home, err)
	}
	path := OwnerPath(c.Home)
	if err := os.WriteFile(path, []byte(configPath+"\n"), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// OwnerPath returns the file that records the owning project of a
// cluster.
func OwnerPath(home string) string {
	return filepath.Join(home, "dbtools-owner")
}

// prunedMarkerName is the file that records that a cluster's runtime has
// already been stopped because its owning project is gone.
const prunedMarkerName = "dbtools-pruned"

// MarkPruned records that the cluster's runtime was stopped because its
// owning project directory is gone.
//
// The marker keeps a later prune from reporting the same stopped cluster
// again. It does not remove the owner record, so a later prune --volumes
// can still find and destroy the data.
func MarkPruned(home string) error {
	path := filepath.Join(home, prunedMarkerName)
	if err := os.WriteFile(path, []byte("stopped by dbtools prune\n"), 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// IsPruned reports whether the cluster's runtime was already stopped by
// a prune.
func IsPruned(home string) bool {
	_, err := os.Stat(filepath.Join(home, prunedMarkerName))
	return err == nil
}

// Owner reads the owning project path of a cluster. It returns an empty
// string when the cluster records no owner.
func Owner(home string) string {
	data, err := os.ReadFile(OwnerPath(home))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
