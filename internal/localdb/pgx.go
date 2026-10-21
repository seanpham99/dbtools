package localdb

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/seanpham99/dbtools/internal/pgx"
)

// pgxProvider hosts the local database in an external pgx runtime.
//
// One cluster exists per project and per engine, so two projects on one
// machine never share a database — the same isolation the docker provider
// gets from per-project containers.
type pgxProvider struct {
	engineName string
}

func (p pgxProvider) Name() string { return ProviderPgX }

// client returns the pgx client for one project.
func (p pgxProvider) client(projectID string) (*pgx.Client, error) {
	return pgx.NewClient(p.engineName, projectID)
}

// Start provisions the cluster and its database, and returns the
// connection URL.
func (p pgxProvider) Start(projectID string, port string, timeout time.Duration, wait bool) (string, error) {
	ctx := context.Background()
	client, err := p.client(projectID)
	if err != nil {
		return "", err
	}
	if err := client.EnsureRunning(ctx, port); err != nil {
		return "", err
	}
	if err := client.EnsureDatabase(ctx, DatabaseName); err != nil {
		return "", err
	}
	localURL, err := client.DatabaseURL(ctx, DatabaseName)
	if err != nil {
		return "", err
	}
	if wait {
		if err := waitForPort(ctx, localURL, timeout); err != nil {
			return "", err
		}
	}
	if err := client.WriteOwner(configPath()); err != nil {
		return "", err
	}
	return localURL, nil
}

// Stop shuts the runtime down. purge also deletes the cluster directory,
// which takes the local database's data with it — the pgx counterpart of
// removing the container's volume.
//
// The socket directory goes with it: it belongs to the cluster, and
// leaving it behind would leak a directory per purged project.
func (p pgxProvider) Stop(projectID string, purge bool) error {
	ctx := context.Background()
	client, err := p.client(projectID)
	if err != nil {
		return err
	}
	if err := client.Stop(ctx); err != nil {
		return err
	}
	if !purge {
		return nil
	}
	if err := os.RemoveAll(client.Home); err != nil {
		return fmt.Errorf("removing the pgx cluster %s: %w", client.Home, err)
	}
	if err := os.RemoveAll(client.RunDir); err != nil {
		return fmt.Errorf("removing the pgx socket directory %s: %w", client.RunDir, err)
	}
	return nil
}

// Restart restarts the runtime, which keeps the cluster's data.
//
// The cluster is ensured first, because pgx refuses to restart a cluster
// that was never created. Restarting then replaces the runtime in one
// step — the runtime is not stopped and started twice.
func (p pgxProvider) Restart(projectID string, port string, timeout time.Duration, wait bool) (string, error) {
	ctx := context.Background()
	client, err := p.client(projectID)
	if err != nil {
		return "", err
	}
	if err := client.EnsureCluster(ctx, port); err != nil {
		return "", err
	}
	if err := client.Restart(ctx); err != nil {
		return "", err
	}
	if err := client.EnsureDatabase(ctx, DatabaseName); err != nil {
		return "", err
	}
	localURL, err := client.DatabaseURL(ctx, DatabaseName)
	if err != nil {
		return "", err
	}
	if wait {
		if err := waitForPort(ctx, localURL, timeout); err != nil {
			return "", err
		}
	}
	return localURL, nil
}

// Logs prints the runtime's server log.
func (p pgxProvider) Logs(projectID string, follow bool) error {
	client, err := p.client(projectID)
	if err != nil {
		return err
	}
	return tailFile(client.LogPath(), follow, os.Stdout)
}

// MaintenanceURL returns the URL of the cluster's maintenance database.
//
// The credentials come from the cluster, never from user configuration,
// so a mistyped target URL cannot aim the drop at a real server. pgx
// generates a random password per cluster and reports it only through
// its own URL command, which is what makes that possible.
//
// The local URL must also point at this cluster, which the port proves:
// the local target's port and the cluster's port are the same number
// only when the target really is this project's database.
func (p pgxProvider) MaintenanceURL(projectID string, localURL string) (string, error) {
	client, err := p.client(projectID)
	if err != nil {
		return "", err
	}
	maintenanceURL, err := client.DatabaseURL(context.Background(), maintenanceDatabase)
	if err != nil {
		return "", fmt.Errorf("reading the pgx cluster's maintenance database URL: %w", err)
	}
	local, err := url.Parse(localURL)
	if err != nil {
		return "", fmt.Errorf("parsing the local database URL: %w", err)
	}
	if !pgx.IsLoopbackHost(local.Hostname()) {
		return "", fmt.Errorf("local target %q does not point at the tool-owned local database (host %q is not loopback); dbtools reset only ever targets the local database, never a remote server", local.Redacted(), local.Hostname())
	}
	maintenance, err := url.Parse(maintenanceURL)
	if err != nil {
		return "", fmt.Errorf("parsing the pgx maintenance database URL: %w", err)
	}
	if local.Port() != maintenance.Port() {
		return "", fmt.Errorf("local target %q does not point at this project's pgx cluster (port %q is not the cluster's port %q); run 'dbtools start' to refresh the local URL", local.Redacted(), local.Port(), maintenance.Port())
	}
	return maintenanceURL, nil
}

// maintenanceDatabase is the database that drop-and-create runs through.
const maintenanceDatabase = "postgres"

// waitForPort blocks until the local database accepts connections.
func waitForPort(ctx context.Context, localURL string, timeout time.Duration) error {
	if timeout <= 0 {
		return nil
	}
	parsed, err := url.Parse(localURL)
	if err != nil {
		return fmt.Errorf("parsing the local database URL: %w", err)
	}
	address := net.JoinHostPort(parsed.Hostname(), parsed.Port())
	deadline := time.Now().Add(timeout)
	for {
		conn, err := net.DialTimeout("tcp", address, time.Second)
		if err == nil {
			conn.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the local database did not accept connections on %s within %v", address, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// tailFile prints a file and, when follow is set, keeps printing the
// lines appended to it.
func tailFile(path string, follow bool, out io.Writer) error {
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("the local database has no log file at %s; start it first", path)
		}
		return fmt.Errorf("reading %s: %w", path, err)
	}
	offset := int64(0)
	for {
		newOffset, err := printFrom(path, offset, out)
		if err != nil {
			return err
		}
		offset = newOffset
		if !follow {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// printFrom prints the bytes of path that follow offset and returns the
// new offset.
func printFrom(path string, offset int64, out io.Writer) (int64, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return offset, nil
		}
		return offset, fmt.Errorf("reading %s: %w", path, err)
	}
	defer file.Close()
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return offset, fmt.Errorf("reading %s: %w", path, err)
	}
	reader := bufio.NewReader(file)
	for {
		line, err := reader.ReadString('\n')
		if line != "" {
			if _, writeErr := fmt.Fprint(out, line); writeErr != nil {
				return offset, writeErr
			}
		}
		if err != nil {
			if err != io.EOF {
				return offset, fmt.Errorf("reading %s: %w", path, err)
			}
			break
		}
	}
	current, err := file.Seek(0, io.SeekCurrent)
	if err != nil {
		return offset, nil
	}
	return current, nil
}

// configPath returns the absolute path of the dbtools.toml in the current
// directory. It records which project owns a pgx cluster, so that prune
// can tell a cluster whose project is gone from one that is still in use.
func configPath() string {
	absolute, err := filepath.Abs("dbtools.toml")
	if err != nil {
		return ""
	}
	return absolute
}
