package dashboard

import (
	"errors"
	"testing"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/statusinfo"
)

func TestBuildRows_MixedSuccessAndErrors(t *testing.T) {
	t.Setenv("DASHBOARD_TEST_LOCAL_URL", "mssql://sa:pw@localhost:1433?database=x")
	// DASHBOARD_TEST_STAGING_URL is deliberately left unset.

	cfg := &config.Config{
		MigrationsDir: "migrations",
		Targets: map[string]config.Target{
			"local":   {URLEnv: "DASHBOARD_TEST_LOCAL_URL"},
			"staging": {URLEnv: "DASHBOARD_TEST_STAGING_URL"},
			"prod":    {URLEnv: "DASHBOARD_TEST_PROD_URL_UNUSED"},
		},
	}
	t.Setenv("DASHBOARD_TEST_PROD_URL_UNUSED", "mssql://sa:pw@localhost:1433?database=y")

	fakeCollect := func(databaseURL, migrationsDir, upSuffix, targetName string) (*statusinfo.Status, error) {
		if targetName == "prod" {
			return nil, errors.New("connection refused")
		}
		return &statusinfo.Status{Target: targetName, HasVersion: true, CurrentVersion: 1, Pending: nil}, nil
	}

	rows := BuildRows(cfg, fakeCollect)
	if len(rows) != 3 {
		t.Fatalf("BuildRows() returned %d rows, want 3: %+v", len(rows), rows)
	}

	byTarget := map[string]Row{}
	for _, r := range rows {
		byTarget[r.Target] = r
	}

	local := byTarget["local"]
	if local.Err != nil || local.Status == nil || local.Status.CurrentVersion != 1 {
		t.Fatalf("local row = %+v, want a successful status", local)
	}

	staging := byTarget["staging"]
	if staging.Err == nil || staging.Status != nil {
		t.Fatalf("staging row = %+v, want a ResolveURL error and nil status", staging)
	}

	prod := byTarget["prod"]
	if prod.Err == nil || prod.Status != nil {
		t.Fatalf("prod row = %+v, want a Collect error and nil status", prod)
	}
}

func TestBuildRows_EmptyConfig(t *testing.T) {
	cfg := &config.Config{MigrationsDir: "migrations", Targets: map[string]config.Target{}}
	rows := BuildRows(cfg, func(string, string, string, string) (*statusinfo.Status, error) {
		t.Fatal("collect should not be called when there are no targets")
		return nil, nil
	})
	if len(rows) != 0 {
		t.Fatalf("BuildRows() = %+v, want empty", rows)
	}
}
