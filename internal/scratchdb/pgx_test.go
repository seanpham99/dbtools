package scratchdb_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/engine"
	_ "github.com/seanpham99/dbtools/internal/engine/postgresengine"
	"github.com/seanpham99/dbtools/internal/pgx"
	"github.com/seanpham99/dbtools/internal/scratchdb"
)

// fakePgX puts a stub pgx on PATH that behaves like the real runtime
// against a fresh cluster directory: start reports "not initialised" until
// init creates the cluster. That is the sequence a real pgx follows, and
// it keeps the scratch provisioning path testable with no pgx installed.
func fakePgX(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the pgx stub is a POSIX shell script")
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "calls.log")
	script := filepath.Join(dir, "pgx")
	body := `#!/bin/sh
echo "$@" >> "` + logPath + `"
case "$*" in
"start")
  if [ -d "$PGX_HOME/data" ]; then exit 0; fi
  echo "pgx: PGX is not initialised" >&2
  exit 3
  ;;
init*)
  mkdir -p "$PGX_HOME/data"
  exit 0
  ;;
create\ *)
  exit 0
  ;;
"--json url "*)
  printf '{"url":"%s://%s@%s:%s/%s"}\n' postgres postgres 127.0.0.1 5433 "$3"
  exit 0
  ;;
stop)
  exit 0
  ;;
esac
exit 0
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(pgx.RootEnv, t.TempDir())
	return logPath
}

func TestPgXScratch_ProvisionsThenDestroysItsOwnCluster(t *testing.T) {
	logPath := fakePgX(t)
	eng, err := engine.ForName("postgres")
	if err != nil {
		t.Fatal(err)
	}

	scratch, err := scratchdb.PgX().Provision(eng, "18")
	if err != nil {
		t.Fatalf("Provision() returned error: %v", err)
	}
	if !strings.Contains(scratch.URL, "dbtools_scratch") {
		t.Fatalf("URL = %q, want it to name the scratch database", scratch.URL)
	}
	if !strings.Contains(scratch.URL, "sslmode=disable") {
		t.Fatalf("URL = %q, want sslmode=disable: the postgres engine requires TLS by default and pgx speaks none", scratch.URL)
	}
	if scratch.Container != "" {
		t.Fatalf("Container = %q, want empty: a pgx scratch has no container", scratch.Container)
	}
	if scratch.Cleanup == nil {
		t.Fatal("Cleanup = nil, want the teardown that removes the scratch cluster")
	}

	calls, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	sequence := string(calls)
	for _, want := range []string{"init --durability ephemeral", "create dbtools_scratch", "--json url dbtools_scratch"} {
		if !strings.Contains(sequence, want) {
			t.Fatalf("pgx calls = %q, want them to include %q", sequence, want)
		}
	}

	root, err := pgx.Root()
	if err != nil {
		t.Fatal(err)
	}
	cluster := onlyScratchCluster(t, root)

	if err := scratch.Cleanup(); err != nil {
		t.Fatalf("Cleanup() returned error: %v", err)
	}
	if _, err := os.Stat(cluster); !os.IsNotExist(err) {
		t.Fatalf("cluster %s still exists after Cleanup (stat error %v)", cluster, err)
	}
	calls, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(calls), "stop") {
		t.Fatalf("pgx calls = %q, want a stop on teardown", string(calls))
	}
}

// onlyScratchCluster returns the one scratch cluster directory a single
// Provision created, proving the scratch got an identity of its own rather
// than reusing a project's local cluster.
func onlyScratchCluster(t *testing.T, root string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "postgres"))
	if err != nil {
		t.Fatalf("reading scratch clusters: %v", err)
	}
	var clusters []string
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "diff-") {
			clusters = append(clusters, filepath.Join(root, "postgres", entry.Name()))
		}
	}
	if len(clusters) != 1 {
		t.Fatalf("scratch clusters = %v, want exactly one diff-* cluster", clusters)
	}
	return clusters[0]
}
