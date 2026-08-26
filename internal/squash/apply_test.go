package squash_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/apply"
	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/diff"
	"github.com/seanpham99/dbtools/internal/engine"
	_ "github.com/seanpham99/dbtools/internal/engine/sqliteengine"
	"github.com/seanpham99/dbtools/internal/ledger"
	"github.com/seanpham99/dbtools/internal/migrator"
	"github.com/seanpham99/dbtools/internal/squash"
)

func TestApplyPlan_RefusesUnverifiedPlan(t *testing.T) {
	plan := &squash.Plan{
		Verified: false,
		Findings: []diff.Finding{{Kind: diff.KindExtra, Object: diff.ObjectTable, Name: "bogus"}},
	}
	_, err := squash.ApplyPlan(nil, "", nil, nil, "", "", "", plan)
	if err == nil {
		t.Fatal("ApplyPlan() with an unverified plan: want error, got nil")
	}
}

func TestApplyPlan_RejectsInvalidBaselineFilename(t *testing.T) {
	plan := &squash.Plan{Verified: true}
	cfg := &config.Config{}
	// Not matching <version>_<name>.up.sql pattern
	if _, err := squash.ApplyPlan(cfg, "local", nil, nil, "", "", "invalid_filename.sql", plan); err == nil {
		t.Fatal("ApplyPlan() with invalid filename: want error, got nil")
	}
	// Non-zero version
	if _, err := squash.ApplyPlan(cfg, "local", nil, nil, "", "", "001_baseline.up.sql", plan); err == nil {
		t.Fatal("ApplyPlan() with non-zero version: want error, got nil")
	}
}

func TestApplyPlan_RejectsExistingBaselineFile(t *testing.T) {
	dir := t.TempDir()
	baselineName := "0000000000000_squashed_baseline.up.sql"
	if err := os.WriteFile(filepath.Join(dir, baselineName), []byte("-- existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := &squash.Plan{Verified: true}
	cfg := &config.Config{MigrationsDir: dir}
	if _, err := squash.ApplyPlan(cfg, "local", nil, nil, dir, filepath.Join(dir, "_archived"), baselineName, plan); err == nil {
		t.Fatal("ApplyPlan() with existing baseline file: want error, got nil")
	}
}

func TestApplyPlan_FreshTargetWritesFilesOnly(t *testing.T) {
	dir := t.TempDir()
	archiveDir := filepath.Join(dir, "_archived")
	f1 := filepath.Join(dir, "1_create_widgets.up.sql")
	f2 := filepath.Join(dir, "2_create_gadgets.up.sql")
	if err := os.WriteFile(f1, []byte("CREATE TABLE widgets (id INTEGER PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f2, []byte("CREATE TABLE gadgets (id INTEGER PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbFile := filepath.Join(t.TempDir(), "target.db")
	targetURL := "sqlite://" + dbFile
	t.Setenv("TEST_SQUASH_LOCAL_URL", targetURL)

	cfg := &config.Config{
		MigrationsDir: dir,
		Migrations: config.MigrationsConfig{
			UpSuffix: ".up.sql",
		},
		Targets: map[string]config.Target{
			"local": {
				Engine: "sqlite",
				URLEnv: "TEST_SQUASH_LOCAL_URL",
			},
		},
	}
	eng, err := engine.ForName("sqlite")
	if err != nil {
		t.Fatal(err)
	}
	d, err := migrator.ReadDir(dir, ".up.sql")
	if err != nil {
		t.Fatal(err)
	}

	plan := &squash.Plan{
		UptoVersion:       2,
		BaselineSQL:       "CREATE TABLE widgets (id INTEGER PRIMARY KEY);\nCREATE TABLE gadgets (id INTEGER PRIMARY KEY);",
		Verified:          true,
		CollapsedVersions: []uint64{1, 2},
	}

	res, err := squash.ApplyPlan(cfg, "local", eng, d, dir, archiveDir, "0000000000000_squashed_baseline.up.sql", plan)
	if err != nil {
		t.Fatalf("ApplyPlan() returned error: %v", err)
	}
	if res.TargetState != squash.TargetFresh {
		t.Errorf("TargetState = %v, want %v", res.TargetState, squash.TargetFresh)
	}

	// Verify baseline file exists
	if _, err := os.Stat(filepath.Join(dir, "0000000000000_squashed_baseline.up.sql")); err != nil {
		t.Errorf("baseline file was not written: %v", err)
	}
	// Verify archived files exist
	if _, err := os.Stat(filepath.Join(archiveDir, "1_create_widgets.up.sql")); err != nil {
		t.Errorf("archived file 1 not found: %v", err)
	}
	if _, err := os.Stat(filepath.Join(archiveDir, "2_create_gadgets.up.sql")); err != nil {
		t.Errorf("archived file 2 not found: %v", err)
	}
	// Verify original files no longer in migrations dir
	if _, err := os.Stat(f1); !os.IsNotExist(err) {
		t.Errorf("file 1 still in migrations dir")
	}
	if _, err := os.Stat(f2); !os.IsNotExist(err) {
		t.Errorf("file 2 still in migrations dir")
	}
}

func TestApplyPlan_FullyAppliedTargetRestamps(t *testing.T) {
	dir := t.TempDir()
	archiveDir := filepath.Join(dir, "_archived")
	f1 := filepath.Join(dir, "1_create_widgets.up.sql")
	f2 := filepath.Join(dir, "2_create_gadgets.up.sql")
	if err := os.WriteFile(f1, []byte("CREATE TABLE widgets (id INTEGER PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f2, []byte("CREATE TABLE gadgets (id INTEGER PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbFile := filepath.Join(t.TempDir(), "target.db")
	targetURL := "sqlite://" + dbFile
	t.Setenv("TEST_SQUASH_LOCAL_URL_2", targetURL)

	cfg := &config.Config{
		MigrationsDir: dir,
		Migrations: config.MigrationsConfig{
			UpSuffix: ".up.sql",
		},
		Targets: map[string]config.Target{
			"local": {
				Engine: "sqlite",
				URLEnv: "TEST_SQUASH_LOCAL_URL_2",
			},
		},
	}
	eng, err := engine.ForName("sqlite")
	if err != nil {
		t.Fatal(err)
	}

	// 1. Fully apply to target first
	if _, err := apply.Run(cfg, "local", ""); err != nil {
		t.Fatalf("initial apply.Run: %v", err)
	}

	d, err := migrator.ReadDir(dir, ".up.sql")
	if err != nil {
		t.Fatal(err)
	}

	baselineSQL := "CREATE TABLE widgets (id INTEGER PRIMARY KEY);\nCREATE TABLE gadgets (id INTEGER PRIMARY KEY);\n"
	plan := &squash.Plan{
		UptoVersion:       2,
		BaselineSQL:       baselineSQL,
		Verified:          true,
		CollapsedVersions: []uint64{1, 2},
	}

	res, err := squash.ApplyPlan(cfg, "local", eng, d, dir, archiveDir, "0000000000000_squashed_baseline.up.sql", plan)
	if err != nil {
		t.Fatalf("ApplyPlan() returned error: %v", err)
	}
	if res.TargetState != squash.TargetRestamped {
		t.Fatalf("TargetState = %v, want %v", res.TargetState, squash.TargetRestamped)
	}

	// Verify target cursor is stamped to version 0
	m, err := migrator.Open(targetURL, dir)
	if err != nil {
		t.Fatal(err)
	}
	ver, dirty, hasVer, err := m.Version()
	m.Close()
	if err != nil {
		t.Fatalf("reading target version: %v", err)
	}
	if !hasVer || dirty || ver != 0 {
		t.Errorf("cursor: hasVersion=%v, dirty=%v, version=%d, want true, false, 0", hasVer, dirty, ver)
	}

	// Verify ledger contains version 0 applied row
	db, err := eng.Open(targetURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	entries, err := eng.Ledger().List(db, config.DefaultLedgerTable)
	if err != nil {
		t.Fatalf("Ledger.List: %v", err)
	}
	foundV0 := false
	for _, e := range entries {
		if e.Version == 0 {
			foundV0 = true
			if e.Status != ledger.StatusApplied {
				t.Errorf("version 0 ledger status = %v, want applied", e.Status)
			}
			if e.HashSource == "adopted" {
				t.Errorf("version 0 ledger HashSource = adopted, want non-adopted for verified squash")
			}
		}
	}
	if !foundV0 {
		t.Error("version 0 ledger entry not found")
	}

	// 2. CRITICAL STEP: Run a real apply.Run against the SAME target again!
	// This proves that golang-migrate's versionExists(0) succeeds and does not error
	// about missing version 2 migration files.
	status, err := apply.Run(cfg, "local", "")
	if err != nil {
		t.Fatalf("second apply.Run after squash failed: %v", err)
	}
	if status.CurrentVersion != 0 {
		t.Errorf("post-apply version = %d, want 0", status.CurrentVersion)
	}
}

func TestApplyPlan_PartiallyAppliedTargetRefuses(t *testing.T) {
	dir := t.TempDir()
	archiveDir := filepath.Join(dir, "_archived")
	f1 := filepath.Join(dir, "1_create_widgets.up.sql")
	f2 := filepath.Join(dir, "2_create_gadgets.up.sql")
	if err := os.WriteFile(f1, []byte("CREATE TABLE widgets (id INTEGER PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f2, []byte("CREATE TABLE gadgets (id INTEGER PRIMARY KEY);"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbFile := filepath.Join(t.TempDir(), "target.db")
	targetURL := "sqlite://" + dbFile
	t.Setenv("TEST_SQUASH_LOCAL_URL_3", targetURL)

	cfg := &config.Config{
		MigrationsDir: dir,
		Migrations: config.MigrationsConfig{
			UpSuffix: ".up.sql",
		},
		Targets: map[string]config.Target{
			"local": {
				Engine: "sqlite",
				URLEnv: "TEST_SQUASH_LOCAL_URL_3",
			},
		},
	}
	eng, err := engine.ForName("sqlite")
	if err != nil {
		t.Fatal(err)
	}

	// Apply only file 1
	m, err := migrator.Open(targetURL, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Step(); err != nil {
		m.Close()
		t.Fatal(err)
	}
	m.Close()

	d, err := migrator.ReadDir(dir, ".up.sql")
	if err != nil {
		t.Fatal(err)
	}

	plan := &squash.Plan{
		UptoVersion:       2,
		BaselineSQL:       "CREATE TABLE widgets (id INTEGER PRIMARY KEY);\nCREATE TABLE gadgets (id INTEGER PRIMARY KEY);",
		Verified:          true,
		CollapsedVersions: []uint64{1, 2},
	}

	_, err = squash.ApplyPlan(cfg, "local", eng, d, dir, archiveDir, "0000000000000_squashed_baseline.up.sql", plan)
	if err == nil {
		t.Fatal("ApplyPlan() on partially applied target: want error, got nil")
	}
	if !strings.Contains(err.Error(), "partially_applied") && !strings.Contains(err.Error(), "between 0 and 2") {
		t.Errorf("error = %q, want mention of partially applied state", err.Error())
	}

	// Verify no baseline written and no files archived
	if _, statErr := os.Stat(filepath.Join(dir, "0000000000000_squashed_baseline.up.sql")); !os.IsNotExist(statErr) {
		t.Error("baseline file was written despite refusal")
	}
	if _, statErr := os.Stat(f1); statErr != nil {
		t.Errorf("file 1 missing from migrations dir: %v", statErr)
	}
}
