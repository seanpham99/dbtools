package pgx

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner records the commands a Client runs and answers them from a
// table. It lets the tests drive the client without a pgx installation.
type fakeRunner struct {
	calls [][]string
	envs  [][]string
	// answers maps a command line (the args after the binary name) to
	// the output the fake returns.
	answers map[string]fakeAnswer
	// default is the answer for a command with no table entry.
	defaultAnswer fakeAnswer
}

type fakeAnswer struct {
	stdout string
	stderr string
	err    error
}

func (f *fakeRunner) run(ctx context.Context, name string, env []string, args ...string) ([]byte, []byte, error) {
	f.calls = append(f.calls, args)
	f.envs = append(f.envs, env)
	key := strings.Join(args, " ")
	answer, ok := f.answers[key]
	if !ok {
		answer = f.defaultAnswer
	}
	return []byte(answer.stdout), []byte(answer.stderr), answer.err
}

// exitError builds the error a Runner returns for a non-zero exit.
func exitError(code int) error {
	return &ExitError{Code: code, Stderr: "pgx: the command failed"}
}

func TestEnsureRunningStartsAnInitializedCluster(t *testing.T) {
	runner := &fakeRunner{
		answers: map[string]fakeAnswer{
			"start": {stdout: "started PGX"},
		},
	}
	client := &Client{Home: filepath.Join(t.TempDir(), "cluster"), RunDir: filepath.Join(t.TempDir(), "run"), Run: runner.run}

	if err := client.EnsureRunning(context.Background(), ""); err != nil {
		t.Fatalf("EnsureRunning() returned error: %v", err)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("EnsureRunning() ran %d commands, want 1: %v", len(runner.calls), runner.calls)
	}
	if got := runner.calls[0][0]; got != "start" {
		t.Fatalf("EnsureRunning() ran %q first, want start", got)
	}
}

func TestEnsureRunningCreatesAMissingCluster(t *testing.T) {
	// The same command ("start") has two different answers — the first
	// fails because the cluster is missing, the second succeeds — so the
	// fake answers by call order.
	var runner fakeRunner
	calls := 0
	client := &Client{
		Home:   filepath.Join(t.TempDir(), "cluster"),
		RunDir: filepath.Join(t.TempDir(), "run"),
		Run: func(ctx context.Context, name string, env []string, args ...string) ([]byte, []byte, error) {
			runner.calls = append(runner.calls, args)
			runner.envs = append(runner.envs, env)
			if len(args) == 1 && args[0] == "start" {
				calls++
				if calls == 1 {
					return nil, []byte("pgx: PGX is not initialised"), exitError(exitNotReady)
				}
				return []byte("started PGX"), nil, nil
			}
			return []byte("initialised PGX"), nil, nil
		},
	}

	if err := client.EnsureRunning(context.Background(), ""); err != nil {
		t.Fatalf("EnsureRunning() returned error: %v", err)
	}
	if len(runner.calls) != 3 {
		t.Fatalf("EnsureRunning() ran %d commands, want 3: %v", len(runner.calls), runner.calls)
	}
	if got := runner.calls[1][0]; got != "init" {
		t.Fatalf("EnsureRunning() ran %q second, want init", got)
	}
}

func TestEnsureRunningPinsThePortOnCreation(t *testing.T) {
	var runner fakeRunner
	starts := 0
	client := &Client{
		Home:   filepath.Join(t.TempDir(), "cluster"),
		RunDir: filepath.Join(t.TempDir(), "run"),
		Run: func(ctx context.Context, name string, env []string, args ...string) ([]byte, []byte, error) {
			runner.calls = append(runner.calls, args)
			if len(args) == 1 && args[0] == "start" {
				starts++
				if starts == 1 {
					return nil, []byte("pgx: PGX is not initialised"), exitError(exitNotReady)
				}
				return []byte("started PGX"), nil, nil
			}
			return []byte("initialised PGX"), nil, nil
		},
	}

	if err := client.EnsureRunning(context.Background(), "5433"); err != nil {
		t.Fatalf("EnsureRunning() returned error: %v", err)
	}
	init := runner.calls[1]
	want := []string{"init", "--durability", "full", "--port", "5433"}
	if len(init) != len(want) {
		t.Fatalf("init args = %v, want %v", init, want)
	}
	for i := range want {
		if init[i] != want[i] {
			t.Fatalf("init args = %v, want %v", init, want)
		}
	}
}

// Every pgx invocation must carry PGX_HOME and PGX_RUN_DIR, so that two
// projects on one machine never share a cluster.
func TestEveryCommandCarriesTheClusterEnvironment(t *testing.T) {
	var runner fakeRunner
	client := &Client{
		Home:   "/tmp/cluster-home",
		RunDir: "/tmp/cluster-run",
		Run: func(ctx context.Context, name string, env []string, args ...string) ([]byte, []byte, error) {
			runner.calls = append(runner.calls, args)
			runner.envs = append(runner.envs, env)
			return []byte("ok"), nil, nil
		},
	}

	if err := client.EnsureRunning(context.Background(), ""); err != nil {
		t.Fatalf("EnsureRunning() returned error: %v", err)
	}
	if err := client.EnsureDatabase(context.Background(), "dbtools_local"); err != nil {
		t.Fatalf("EnsureDatabase() returned error: %v", err)
	}
	if len(runner.envs) < 2 {
		t.Fatalf("ran %d commands, want at least 2", len(runner.envs))
	}
	for i, env := range runner.envs {
		joined := strings.Join(env, " ")
		if !strings.Contains(joined, "PGX_HOME=/tmp/cluster-home") {
			t.Fatalf("command %d env %q does not set PGX_HOME", i, joined)
		}
		if !strings.Contains(joined, "PGX_RUN_DIR=/tmp/cluster-run") {
			t.Fatalf("command %d env %q does not set PGX_RUN_DIR", i, joined)
		}
	}
}

// pgx uses one exit code for two different conflicts: an
// already-initialised cluster (benign here) and a port that is already
// bound (a real failure). Only the first may be treated as success.
func TestEnsureClusterTreatsAnAlreadyInitialisedClusterAsSuccess(t *testing.T) {
	client := &Client{
		Home:   t.TempDir(),
		RunDir: t.TempDir(),
		Run: func(ctx context.Context, name string, env []string, args ...string) ([]byte, []byte, error) {
			return nil, []byte("pgx: PGX is already initialised at /tmp/cluster\n  hint: `pgx start` to use it"), &ExitError{Code: exitConflict, Stderr: "pgx: PGX is already initialised at /tmp/cluster"}
		},
	}
	if err := client.EnsureCluster(context.Background(), ""); err != nil {
		t.Fatalf("EnsureCluster() on an already-initialised cluster returned error: %v", err)
	}
}

func TestEnsureClusterReportsABoundPort(t *testing.T) {
	client := &Client{
		Home:   t.TempDir(),
		RunDir: t.TempDir(),
		Run: func(ctx context.Context, name string, env []string, args ...string) ([]byte, []byte, error) {
			diagnostic := "pgx: port 55432 is in use (bind 127.0.0.1:55432: Address already in use (os error 98))\n  hint: choose another with `pgx init --port N`"
			return nil, []byte(diagnostic), &ExitError{Code: exitConflict, Stderr: diagnostic}
		},
	}
	err := client.EnsureCluster(context.Background(), "55432")
	if err == nil {
		t.Fatal("EnsureCluster() on a bound port returned nil error, want error")
	}
	if !strings.Contains(err.Error(), "in use") {
		t.Fatalf("error %q does not say the port is in use", err)
	}
}

func TestEnsureDatabaseTreatsAnExistingDatabaseAsSuccess(t *testing.T) {
	client := &Client{
		Home:   t.TempDir(),
		RunDir: t.TempDir(),
		Run: func(ctx context.Context, name string, env []string, args ...string) ([]byte, []byte, error) {
			return nil, []byte(`pgx: cannot create database dbtools_local: database "dbtools_local" already exists`), exitError(exitConflict)
		},
	}
	if err := client.EnsureDatabase(context.Background(), "dbtools_local"); err != nil {
		t.Fatalf("EnsureDatabase() on an existing database returned error: %v", err)
	}
}

func TestEnsureDatabaseReportsAFailure(t *testing.T) {
	client := &Client{
		Home:   t.TempDir(),
		RunDir: t.TempDir(),
		Run: func(ctx context.Context, name string, env []string, args ...string) ([]byte, []byte, error) {
			return nil, []byte("pgx: disk full"), exitError(1)
		},
	}
	err := client.EnsureDatabase(context.Background(), "dbtools_local")
	if err == nil {
		t.Fatal("EnsureDatabase() returned nil error, want error")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("error %q does not carry pgx's diagnostic", err)
	}
}

// pgx speaks no TLS, and dbtools' postgres engine requires it by
// default, so the URL must carry sslmode=disable.
func TestDatabaseURLAddsSSLModeDisable(t *testing.T) {
	payload, err := json.Marshal(map[string]string{
		"url": "postgres://postgres:***@127.0.0.1:5433/dbtools_local",
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &Client{
		Home:   t.TempDir(),
		RunDir: t.TempDir(),
		Run: func(ctx context.Context, name string, env []string, args ...string) ([]byte, []byte, error) {
			return payload, nil, nil
		},
	}
	got, err := client.DatabaseURL(context.Background(), "dbtools_local")
	if err != nil {
		t.Fatalf("DatabaseURL() returned error: %v", err)
	}
	if !strings.Contains(got, "sslmode=disable") {
		t.Fatalf("DatabaseURL() = %q, want it to carry sslmode=disable", got)
	}
}

func TestDatabaseURLRejectsANonLoopbackHost(t *testing.T) {
	payload := []byte(`{"url":"postgres://postgres:***@db.example.com:5432/dbtools_local"}`)
	client := &Client{
		Home:   t.TempDir(),
		RunDir: t.TempDir(),
		Run: func(ctx context.Context, name string, env []string, args ...string) ([]byte, []byte, error) {
			return payload, nil, nil
		},
	}
	if _, err := client.DatabaseURL(context.Background(), "dbtools_local"); err == nil {
		t.Fatal("DatabaseURL() with a non-loopback host returned nil error, want error")
	}
}

func TestStopTreatsAStoppedRuntimeAsSuccess(t *testing.T) {
	client := &Client{
		Home:   t.TempDir(),
		RunDir: t.TempDir(),
		Run: func(ctx context.Context, name string, env []string, args ...string) ([]byte, []byte, error) {
			return nil, []byte("pgx: PGX is not running"), exitError(exitNotReady)
		},
	}
	if err := client.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() on a stopped runtime returned error: %v", err)
	}
}

func TestStopReportsAFailure(t *testing.T) {
	client := &Client{
		Home:   t.TempDir(),
		RunDir: t.TempDir(),
		Run: func(ctx context.Context, name string, env []string, args ...string) ([]byte, []byte, error) {
			return nil, []byte("pgx: cannot write to the data directory"), exitError(1)
		},
	}
	if err := client.Stop(context.Background()); err == nil {
		t.Fatal("Stop() returned nil error, want error")
	}
}

func TestExitErrorCarriesTheCode(t *testing.T) {
	runner := &fakeRunner{
		defaultAnswer: fakeAnswer{stderr: "pgx: not initialised", err: exitError(exitNotReady)},
	}
	client := &Client{Home: t.TempDir(), RunDir: t.TempDir(), Run: runner.run}
	_, _, err := client.run(context.Background(), "status")
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("run() error %v is not an *ExitError", err)
	}
	if exitErr.Code != exitNotReady {
		t.Fatalf("ExitError.Code = %d, want %d", exitErr.Code, exitNotReady)
	}
}

func TestOwnerRoundTrip(t *testing.T) {
	home := t.TempDir()
	client := &Client{Home: home, RunDir: t.TempDir()}
	if err := client.WriteOwner("/tmp/project/dbtools.toml"); err != nil {
		t.Fatalf("WriteOwner() returned error: %v", err)
	}
	if got := Owner(home); got != "/tmp/project/dbtools.toml" {
		t.Fatalf("Owner() = %q, want /tmp/project/dbtools.toml", got)
	}
}

func TestOwnerOfAClusterWithNoOwnerIsEmpty(t *testing.T) {
	if got := Owner(t.TempDir()); got != "" {
		t.Fatalf("Owner() = %q, want empty", got)
	}
}

// The cluster and the socket directory are separate, because pgx needs a
// short path for the socket file.
func TestPathsKeepsTheSocketDirectorySeparate(t *testing.T) {
	t.Setenv(RootEnv, "/tmp/pgx-root")
	home, runDir, err := Paths("postgres", "abc123")
	if err != nil {
		t.Fatalf("Paths() returned error: %v", err)
	}
	if home != filepath.Join("/tmp/pgx-root", "postgres", "abc123") {
		t.Fatalf("Paths() home = %q", home)
	}
	if runDir != filepath.Join("/tmp/pgx-root", "run", "postgres", "abc123") {
		t.Fatalf("Paths() runDir = %q", runDir)
	}
	if home == runDir {
		t.Fatal("Paths() returned the same directory for the cluster and the socket")
	}
}
