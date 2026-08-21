package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/seanpham99/dbtools/internal/apply"
	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine"
)

func TestPlanCmd_ExitCodeContract(t *testing.T) {
	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
		jsonOutput = false
		planTarget = ""
		planURL = ""
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll("migrations", 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join("migrations", "20260101000000_init.up.sql"), []byte("CREATE TABLE items (id INT);"), 0o644)

	dbURL := "sqlite://" + filepath.Join(dir, "plan_test.db")
	t.Setenv("DBTOOLS_TEST_PLAN_URL", dbURL)

	configContent := `migrations_dir = "migrations"

[targets.local]
url_env = "DBTOOLS_TEST_PLAN_URL"
`
	if err := os.WriteFile("dbtools.toml", []byte(configContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// 1. With pending migrations: plan must return exit code 2
	planTarget = "local"
	planURL = ""
	err = planCmd.RunE(planCmd, nil)
	if err == nil {
		t.Fatal("expected exit code 2 error for pending migrations, got nil")
	}
	var exitErr *ExitCodeError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("expected ExitCodeError with Code=2, got %v", err)
	}

	// 2. Apply migration
	cfg, err := config.Load("dbtools.toml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := apply.Run(cfg, "local", ""); err != nil {
		t.Fatalf("apply.Run() failed: %v", err)
	}

	// 3. Clean state: plan must return exit code 0 (nil)
	err = planCmd.RunE(planCmd, nil)
	if err != nil {
		t.Fatalf("expected clean plan exit 0 (nil), got %v", err)
	}

	// 4. Introduce drift by dropping table out of band: plan must return exit code 2
	eng, err := engine.ForTarget("sqlite", dbURL)
	if err != nil {
		t.Fatal(err)
	}
	db, err := eng.Open(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("DROP TABLE items;"); err != nil {
		t.Fatal(err)
	}

	err = planCmd.RunE(planCmd, nil)
	if err == nil {
		t.Fatal("expected exit code 2 error for drifted database, got nil")
	}
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("expected ExitCodeError with Code=2 on drift, got %v", err)
	}

	// 5. Verify also returns exit code 2 on drift
	err = verifyCmd.RunE(verifyCmd, []string{"local"})
	if err == nil {
		t.Fatal("expected verify to return error on drift, got nil")
	}
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("expected verify ExitCodeError with Code=2 on drift, got %v", err)
	}
}
