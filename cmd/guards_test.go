package cmd

import (
	"strings"
	"testing"

	"github.com/dbtools/dbtools/internal/apply"
	"github.com/dbtools/dbtools/internal/config"
	"github.com/dbtools/dbtools/internal/statusinfo"
)

func TestUpRefusesNonLocalTarget(t *testing.T) {
	// C1: `up --target prod` must refuse before touching anything — the
	// push preview+--yes path is the only way to reach a remote target.
	upTarget = "prod"
	defer func() { upTarget = "local" }()

	// Any attempt to load config that would lead to apply.Run is a test
	// failure: the guard must fire before config is even needed.
	loadConfig = func(string) (*config.Config, error) {
		t.Fatal("config loaded — the guard must fire before config resolution")
		return nil, nil
	}
	defer func() { loadConfig = config.Load }()

	cmd := upCmd
	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("up --target prod should refuse")
	}
	if !strings.Contains(err.Error(), "use `push prod --yes`") {
		t.Fatalf("error = %v, want it to point at push --yes", err)
	}
}

func TestUpAllowsLocalTarget(t *testing.T) {
	// The guard must not break the normal local dev loop. We stub apply.Run
	// so no real database is touched.
	upTarget = "local"
	defer func() { upTarget = "local" }()

	loadConfig = func(string) (*config.Config, error) {
		return &config.Config{MigrationsDir: "migrations", Targets: map[string]config.Target{"local": {URLEnv: "DBTOOLS_LOCAL_URL"}}}, nil
	}
	defer func() { loadConfig = config.Load }()
	applyRun = func(cfg *config.Config, targetName string, _ string) (*statusinfo.Status, error) {
		if targetName != "local" {
			t.Fatalf("apply.Run called for %q, want local", targetName)
		}
		return &statusinfo.Status{Target: "local", CurrentVersion: 1}, nil
	}
	defer func() { applyRun = apply.Run }()

	cmd := upCmd
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("up (local) returned error: %v", err)
	}
}

func TestPushRefusesProtectedTarget(t *testing.T) {
	// Protected target: push must refuse before the --yes check.
	loadConfig = func(string) (*config.Config, error) {
		return &config.Config{
			MigrationsDir: "migrations",
			Targets: map[string]config.Target{
				"prod": {URLEnv: "DBTOOLS_PROD_URL", Protected: true},
			},
		}, nil
	}
	defer func() { loadConfig = config.Load }()
	pushYes = true
	defer func() { pushYes = false }()

	err := runPush("prod")
	if err == nil {
		t.Fatal("push to a protected target should refuse")
	}
	if !strings.Contains(err.Error(), "protected") {
		t.Fatalf("error = %v, want it to mention protected", err)
	}
}

func TestRepairRefusesProtectedTarget(t *testing.T) {
	loadConfig = func(string) (*config.Config, error) {
		return &config.Config{
			MigrationsDir: "migrations",
			Targets: map[string]config.Target{
				"prod": {URLEnv: "DBTOOLS_PROD_URL", Protected: true},
			},
		}, nil
	}
	defer func() { loadConfig = config.Load }()
	repairYes = true
	defer func() { repairYes = false }()

	err := runRepair("prod", nil)
	if err == nil {
		t.Fatal("repair on a protected target should refuse")
	}
	if !strings.Contains(err.Error(), "protected") {
		t.Fatalf("error = %v, want it to mention protected", err)
	}
}

func TestStatusURLScoping(t *testing.T) {
	// H1 regression: `status --url $X --target prod` must apply the URL
	// only to prod — never to every target in the loop.
	loadConfig = func(string) (*config.Config, error) {
		return &config.Config{
			MigrationsDir: "migrations",
			Targets: map[string]config.Target{
				"local": {URLEnv: "DBTOOLS_LOCAL_URL"},
				"prod":  {URLEnv: "DBTOOLS_PROD_URL"},
			},
		}, nil
	}
	defer func() { loadConfig = config.Load }()

	statusTarget = "prod"
	statusURL = "sqlserver://override"
	defer func() { statusTarget = ""; statusURL = "" }()

	cfg, err := loadConfig("dbtools.toml")
	if err != nil {
		t.Fatal(err)
	}
	statuses, failures := collectStatuses(cfg)
	// prod resolves with the override URL; local must NOT appear at all
	// (--target narrows the iteration).
	if len(statuses)+len(failures) != 1 {
		t.Fatalf("collectStatuses returned %d statuses + %d failures, want exactly prod (1)", len(statuses), len(failures))
	}
	// prod should fail to connect (override URL unreachable) — that's fine;
	// the point is it was the ONLY target attempted, and the URL override
	// reached only it.
	if len(failures) != 1 || failures[0].Target != "prod" {
		t.Fatalf("failures = %+v, want exactly one prod failure (engine mismatch/unreachable)", failures)
	}
}
