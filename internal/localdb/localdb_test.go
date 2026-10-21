package localdb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/pgx"
)

func TestProviderNameDefaultsToDocker(t *testing.T) {
	if got := ProviderName(nil); got != ProviderDocker {
		t.Fatalf("ProviderName(nil) = %q, want %q", got, ProviderDocker)
	}
	if got := ProviderName(&config.Config{}); got != ProviderDocker {
		t.Fatalf("ProviderName(empty config) = %q, want %q", got, ProviderDocker)
	}
}

func TestProviderNameReadsTheSetting(t *testing.T) {
	cfg := &config.Config{Local: config.LocalConfig{Provider: "pgx"}}
	if got := ProviderName(cfg); got != ProviderPgX {
		t.Fatalf("ProviderName() = %q, want %q", got, ProviderPgX)
	}
}

func TestProviderNameIsCaseAndSpaceInsensitive(t *testing.T) {
	cfg := &config.Config{Local: config.LocalConfig{Provider: "  PGX  "}}
	if got := ProviderName(cfg); got != ProviderPgX {
		t.Fatalf("ProviderName() = %q, want %q", got, ProviderPgX)
	}
}

func TestSelectRejectsAnUnknownProvider(t *testing.T) {
	cfg := &config.Config{Local: config.LocalConfig{Provider: "podman"}}
	_, err := Select(cfg, "postgres")
	if err == nil {
		t.Fatal("Select() with an unknown provider returned nil error, want error")
	}
	if !strings.Contains(err.Error(), "podman") {
		t.Fatalf("error %q does not name the unknown provider", err)
	}
}

// The pgx provider speaks the PostgreSQL wire protocol, so it cannot
// serve the other engines.
func TestSelectRejectsPgXForANonPostgresEngine(t *testing.T) {
	cfg := &config.Config{Local: config.LocalConfig{Provider: ProviderPgX}}
	_, err := Select(cfg, "mssql")
	if err == nil {
		t.Fatal("Select() with pgx and the mssql engine returned nil error, want error")
	}
	if !strings.Contains(err.Error(), "postgres") {
		t.Fatalf("error %q does not name the engine that pgx can serve", err)
	}
}

func TestSelectReturnsTheDockerProviderByDefault(t *testing.T) {
	p, err := Select(&config.Config{}, "mssql")
	if err != nil {
		t.Fatalf("Select() returned error: %v", err)
	}
	if p.Name() != ProviderDocker {
		t.Fatalf("Select() returned the %q provider, want %q", p.Name(), ProviderDocker)
	}
}

// A project directory with no dbtools.toml must still find a provider, so
// that stop and logs keep working in a bare directory.
func TestSelectWithNoConfigFallsBackToDocker(t *testing.T) {
	p, err := Select(nil, "mssql")
	if err != nil {
		t.Fatalf("Select(nil) returned error: %v", err)
	}
	if p.Name() != ProviderDocker {
		t.Fatalf("Select(nil) returned the %q provider, want %q", p.Name(), ProviderDocker)
	}
}

// fakePgX puts a stub pgx on PATH. The stub answers every command with
// "not running" (exit 3), which is what a stopped cluster reports, so
// the prune path runs for real without a pgx installation.
func fakePgX(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "pgx")
	body := "#!/bin/sh\nexit 3\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestPruneRemovesAClusterWhoseProjectIsGone(t *testing.T) {
	fakePgX(t)
	root := t.TempDir()
	t.Setenv(pgx.RootEnv, root)

	home := filepath.Join(root, "postgres", "abc123")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	// The owner points at a dbtools.toml that no longer exists.
	if err := os.WriteFile(filepath.Join(home, "dbtools-owner"), []byte(filepath.Join(root, "gone", "dbtools.toml")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := Prune(true, false)
	if err != nil {
		t.Fatalf("Prune() returned error: %v", err)
	}
	if len(report.Clusters) != 1 {
		t.Fatalf("Prune() removed %d clusters, want 1: %v", len(report.Clusters), report.Clusters)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("the cluster directory still exists after Prune(): %v", err)
	}
}

func TestPruneKeepsAClusterWhoseProjectStillExists(t *testing.T) {
	fakePgX(t)
	root := t.TempDir()
	t.Setenv(pgx.RootEnv, root)

	projectDir := t.TempDir()
	home := filepath.Join(root, "postgres", "abc123")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "dbtools-owner"), []byte(filepath.Join(projectDir, "dbtools.toml")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "dbtools.toml"), []byte("[targets.local]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := Prune(true, false)
	if err != nil {
		t.Fatalf("Prune() returned error: %v", err)
	}
	if len(report.Clusters) != 0 {
		t.Fatalf("Prune() removed %v, want nothing", report.Clusters)
	}
	if _, err := os.Stat(home); err != nil {
		t.Fatalf("the cluster directory is gone: %v", err)
	}
}

// A cluster with no owner record cannot be proven dead, so it is left
// alone — the same rule the container prune follows.
func TestPruneLeavesAClusterWithNoOwnerRecord(t *testing.T) {
	fakePgX(t)
	root := t.TempDir()
	t.Setenv(pgx.RootEnv, root)

	home := filepath.Join(root, "postgres", "abc123")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}

	report, err := Prune(true, false)
	if err != nil {
		t.Fatalf("Prune() returned error: %v", err)
	}
	if len(report.Clusters) != 0 {
		t.Fatalf("Prune() removed %v, want nothing", report.Clusters)
	}
}

func TestPruneDryRunRemovesNothing(t *testing.T) {
	fakePgX(t)
	root := t.TempDir()
	t.Setenv(pgx.RootEnv, root)

	home := filepath.Join(root, "postgres", "abc123")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "dbtools-owner"), []byte(filepath.Join(root, "gone", "dbtools.toml")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := Prune(true, true)
	if err != nil {
		t.Fatalf("Prune() returned error: %v", err)
	}
	if len(report.Clusters) != 1 {
		t.Fatalf("the dry run reported %d clusters, want 1: %v", len(report.Clusters), report.Clusters)
	}
	if _, err := os.Stat(home); err != nil {
		t.Fatalf("the dry run removed the cluster directory: %v", err)
	}
}

// Without --volumes the data survives, the way a pruned container's
// volume does.
func TestPruneKeepsTheDataWithoutRemoveData(t *testing.T) {
	fakePgX(t)
	root := t.TempDir()
	t.Setenv(pgx.RootEnv, root)

	home := filepath.Join(root, "postgres", "abc123")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "dbtools-owner"), []byte(filepath.Join(root, "gone", "dbtools.toml")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := Prune(false, false)
	if err != nil {
		t.Fatalf("Prune() returned error: %v", err)
	}
	if len(report.StoppedClusters) != 1 {
		t.Fatalf("Prune() stopped %d clusters, want 1: %v", len(report.StoppedClusters), report.StoppedClusters)
	}
	if len(report.Clusters) != 0 {
		t.Fatalf("Prune() removed %v, want nothing removed", report.Clusters)
	}
	if _, err := os.Stat(home); err != nil {
		t.Fatalf("the cluster directory is gone: %v", err)
	}
}

// A host without pgx has nothing to clean up, so the pgx half is skipped
// rather than failing the command.
func TestPruneSkipsWhenPgXIsNotInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	report, err := Prune(true, false)
	if err != nil {
		t.Fatalf("Prune() returned error: %v", err)
	}
	if !report.Skipped {
		t.Fatal("Prune() did not report the pgx cleanup as skipped")
	}
	if report.Reason == "" {
		t.Fatal("Prune() skipped without saying why")
	}
}

// A cluster whose runtime a previous prune already stopped is not
// reported again — the leak is closed, and repeating it would be noise.
func TestPruneDoesNotReportAnAlreadyStoppedClusterTwice(t *testing.T) {
	fakePgX(t)
	root := t.TempDir()
	t.Setenv(pgx.RootEnv, root)

	home := filepath.Join(root, "postgres", "abc123")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "dbtools-owner"), []byte(filepath.Join(root, "gone", "dbtools.toml")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	first, err := Prune(false, false)
	if err != nil {
		t.Fatalf("Prune() returned error: %v", err)
	}
	if len(first.StoppedClusters) != 1 {
		t.Fatalf("the first prune stopped %d clusters, want 1", len(first.StoppedClusters))
	}
	second, err := Prune(false, false)
	if err != nil {
		t.Fatalf("the second Prune() returned error: %v", err)
	}
	if len(second.StoppedClusters) != 0 {
		t.Fatalf("the second prune reported %v again, want nothing", second.StoppedClusters)
	}
	// The data survives, so a prune --volumes can still destroy it.
	if _, err := os.Stat(home); err != nil {
		t.Fatalf("the cluster directory is gone: %v", err)
	}
	third, err := Prune(true, false)
	if err != nil {
		t.Fatalf("the third Prune() returned error: %v", err)
	}
	if len(third.Clusters) != 1 {
		t.Fatalf("the prune with removeData reported %d clusters, want 1", len(third.Clusters))
	}
}

func TestPruneWithNoClustersIsANoop(t *testing.T) {
	fakePgX(t)
	t.Setenv(pgx.RootEnv, filepath.Join(t.TempDir(), "never-created"))
	report, err := Prune(true, false)
	if err != nil {
		t.Fatalf("Prune() returned error: %v", err)
	}
	if len(report.Clusters) != 0 || len(report.StoppedClusters) != 0 {
		t.Fatalf("Prune() reported %v and %v, want nothing", report.Clusters, report.StoppedClusters)
	}
}
