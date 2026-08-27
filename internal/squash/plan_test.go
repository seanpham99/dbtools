package squash_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine"
	_ "github.com/seanpham99/dbtools/internal/engine/sqliteengine"
	"github.com/seanpham99/dbtools/internal/squash"
)

func TestBuildPlan_CleanHistoryVerifies(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "1_create_widgets.up.sql"), []byte("CREATE TABLE widgets (id INTEGER PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2_create_gadgets.up.sql"), []byte("CREATE TABLE gadgets (id INTEGER PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		MigrationsDir: dir,
		Migrations: config.MigrationsConfig{
			UpSuffix: ".up.sql",
		},
		Targets: map[string]config.Target{
			"squash-scratch-replay": {
				Engine: "sqlite",
			},
		},
	}
	eng, err := engine.ForName("sqlite")
	if err != nil {
		t.Fatal(err)
	}

	plan, err := squash.BuildPlan(cfg, eng, dir, ".up.sql", 2, "", false)
	if err != nil {
		t.Fatalf("BuildPlan() returned error: %v", err)
	}
	if !plan.Verified {
		t.Fatalf("plan.Verified = false, Findings = %+v, want a clean verify", plan.Findings)
	}
	if !strings.Contains(plan.BaselineSQL, "widgets") || !strings.Contains(plan.BaselineSQL, "gadgets") {
		t.Errorf("BaselineSQL = %q, want it to contain both tables", plan.BaselineSQL)
	}
	want := []uint64{1, 2}
	if len(plan.CollapsedVersions) != len(want) || plan.CollapsedVersions[0] != 1 || plan.CollapsedVersions[1] != 2 {
		t.Errorf("CollapsedVersions = %v, want %v", plan.CollapsedVersions, want)
	}
}

func TestBuildPlan_PartialUptoReplaysOnlyCollapsed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "1_create_widgets.up.sql"), []byte("CREATE TABLE widgets (id INTEGER PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2_create_gadgets.up.sql"), []byte("CREATE TABLE gadgets (id INTEGER PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		MigrationsDir: dir,
		Migrations: config.MigrationsConfig{
			UpSuffix: ".up.sql",
		},
		Targets: map[string]config.Target{
			"squash-scratch-replay": {
				Engine: "sqlite",
			},
		},
	}
	eng, err := engine.ForName("sqlite")
	if err != nil {
		t.Fatal(err)
	}

	plan, err := squash.BuildPlan(cfg, eng, dir, ".up.sql", 1, "", false)
	if err != nil {
		t.Fatalf("BuildPlan() returned error: %v", err)
	}
	if !plan.Verified {
		t.Fatalf("plan.Verified = false, Findings = %+v", plan.Findings)
	}
	if !strings.Contains(plan.BaselineSQL, "widgets") {
		t.Errorf("BaselineSQL = %q, want widgets", plan.BaselineSQL)
	}
	if strings.Contains(plan.BaselineSQL, "gadgets") {
		t.Errorf("BaselineSQL = %q, should NOT contain gadgets for upto=1", plan.BaselineSQL)
	}
	if len(plan.CollapsedVersions) != 1 || plan.CollapsedVersions[0] != 1 {
		t.Errorf("CollapsedVersions = %v, want [1]", plan.CollapsedVersions)
	}
}
