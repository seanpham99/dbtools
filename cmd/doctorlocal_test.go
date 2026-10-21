package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/config"
)

func localTargetConfig(engine, provider, major string) *config.Config {
	return &config.Config{
		Targets: map[string]config.Target{"local": {URLEnv: "DBTOOLS_LOCAL_URL", Engine: engine}},
		Local:   config.LocalConfig{Provider: provider, PostgresMajor: config.PostgresMajor(major)},
	}
}

func TestLocalVersionCheck_SkippedWithoutAPostgresLocalTarget(t *testing.T) {
	cfg := &config.Config{Targets: map[string]config.Target{
		"prod": {URLEnv: "PROD_URL", Engine: "postgres"},
	}}
	if _, ok := localVersionCheck(cfg, "postgres", "18"); ok {
		t.Fatal("localVersionCheck() reported a check for a project with no local target")
	}

	sqlite := &config.Config{Targets: map[string]config.Target{
		"local": {URLEnv: "DBTOOLS_LOCAL_URL", Engine: "sqlite"},
	}}
	if _, ok := localVersionCheck(sqlite, "sqlite", ""); ok {
		t.Fatal("localVersionCheck() reported a check for a sqlite local database")
	}
}

// The provider's artifact is the evidence; before a local database exists
// the configured major is only a prediction and is reported as one.
func TestLocalVersionResult_ReportsAPredictionBeforeTheDatabaseExists(t *testing.T) {
	check := localVersionResult("17", "docker", "", false, "17", nil)
	if check.Status != "ok" {
		t.Fatalf("status = %q, want ok", check.Status)
	}
	if !strings.Contains(check.Message, "not created yet") || !strings.Contains(check.Message, "17") {
		t.Fatalf("message = %q, want it to say the database is not created yet and will run 17", check.Message)
	}
}

func TestLocalVersionResult_WarnsWhenTheMajorsDiffer(t *testing.T) {
	check := localVersionResult("17", "docker", "17", true, "18", nil)
	if check.Name != "local-version" {
		t.Fatalf("check name = %q, want local-version", check.Name)
	}
	if check.Status != "warn" {
		t.Fatalf("status = %q, want warn", check.Status)
	}
	for _, want := range []string{"17", "18", "docker"} {
		if !strings.Contains(check.Message, want) {
			t.Fatalf("message %q does not mention %q", check.Message, want)
		}
	}
}

// A pin that has not been applied yet must not read as healthy: the data
// volume belongs to the major that wrote it, whatever the configuration
// now asks for.
func TestLocalVersionResult_WarnsWhenTheDataOutranksThePin(t *testing.T) {
	check := localVersionResult("18", "docker", "17", true, "18", nil)
	if check.Status != "warn" {
		t.Fatalf("status = %q, want warn", check.Status)
	}
	for _, want := range []string{"17", "18", "stop --no-backup"} {
		if !strings.Contains(check.Message, want) {
			t.Fatalf("message %q does not mention %q", check.Message, want)
		}
	}
}

func TestLocalVersionResult_ReportsAMatchAsHealthy(t *testing.T) {
	check := localVersionResult("17", "docker", "17", true, "17", nil)
	if check.Status != "ok" {
		t.Fatalf("status = %q, want ok", check.Status)
	}
	if !strings.Contains(check.Message, "same major") {
		t.Fatalf("message = %q, want it to name the shared major", check.Message)
	}
}

// A pin the provider cannot serve is a warning here, never a failure: the
// local database belongs to the project, not to the target being audited.
func TestLocalVersionResult_WarnsOnAnUnservablePin(t *testing.T) {
	check := localVersionResult("", "pgx", "", false, "18", errors.New("the pgx local provider serves PostgreSQL 18 only, but dbtools.toml pins [local] postgres_major = \"17\""))
	if check.Status != "warn" {
		t.Fatalf("status = %q, want warn", check.Status)
	}
	if !strings.Contains(check.Message, "cannot determine the local database's major") {
		t.Fatalf("message = %q, want it to say the local major is undeterminable", check.Message)
	}
}

// The wiring still answers "is this project's local database a postgres
// one", which is what decides whether the check appears at all.
func TestLocalVersionCheck_WiresAPostgresLocalTarget(t *testing.T) {
	if _, ok := localVersionCheck(localTargetConfig("postgres", "docker", "17"), "postgres", "17"); !ok {
		t.Fatal("localVersionCheck() reported no check for a postgres local target")
	}
}

// A project can have a postgres local database and a MySQL target. The
// MySQL version string is not a postgres major, so it must not be compared
// with one — the local major is reported on its own.
func TestLocalVersionCheck_DoesNotCompareAgainstANonPostgresTarget(t *testing.T) {
	check, ok := localVersionCheck(localTargetConfig("postgres", "docker", "17"), "mysql", "8.0")
	if !ok {
		t.Fatal("localVersionCheck() reported no check")
	}
	if check.Status != "ok" {
		t.Fatalf("status = %q, want ok: a MySQL version is not a comparable major", check.Status)
	}
	if strings.Contains(check.Message, "target:") || strings.Contains(check.Message, "8.0") {
		t.Fatalf("message = %q, want no comparison with the MySQL version", check.Message)
	}
	if !strings.Contains(check.Message, "postgres 17") {
		t.Fatalf("message = %q, want the local major reported", check.Message)
	}
}
