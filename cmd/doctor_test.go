package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/apply"
	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine/sqliteengine"
)

func setupTestDoctorEnv(t *testing.T) (string, string, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "doctor_test.db")
	rawURL := fmt.Sprintf("sqlite://%s", dbPath)
	migrationsDir := filepath.Join(dir, "migrations")
	if err := os.MkdirAll(migrationsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	m1Up := `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);`
	m1Down := `DROP TABLE users;`
	if err := os.WriteFile(filepath.Join(migrationsDir, "20260822000001_users.up.sql"), []byte(m1Up), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(migrationsDir, "20260822000001_users.down.sql"), []byte(m1Down), 0o644); err != nil {
		t.Fatal(err)
	}

	cfgContent := fmt.Sprintf(`migrations_dir = %q
[targets.testdb]
url_env = "DBTOOLS_TEST_DOCTOR_URL"
engine = "sqlite"
protected = false
`, migrationsDir)

	configPath := filepath.Join(dir, "dbtools.toml")
	if err := os.WriteFile(configPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("DBTOOLS_TEST_DOCTOR_URL", rawURL)

	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}

	return dir, rawURL, cfg
}

func TestDoctorHealthy(t *testing.T) {
	dir, _, cfg := setupTestDoctorEnv(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Apply migrations via apply.Run so ledger is fully initialized with content hash
	_, err = apply.Run(cfg, "testdb", "")
	if err != nil {
		t.Fatalf("apply.Run() err=%v", err)
	}

	report := evaluateTarget(cfg, "testdb")
	if !report.Healthy {
		t.Fatalf("evaluateTarget() healthy = false, want true. Checks: %+v", report.Checks)
	}
	if report.Exit != 0 {
		t.Fatalf("evaluateTarget() exit = %d, want 0", report.Exit)
	}

	// Run runDoctor directly
	jsonOutput = false
	err = runDoctor("testdb")
	if err != nil {
		t.Fatalf("runDoctor() unexpected error: %v", err)
	}
}

func TestDoctorDriftContentHash(t *testing.T) {
	dir, _, cfg := setupTestDoctorEnv(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Apply migration first
	_, err = apply.Run(cfg, "testdb", "")
	if err != nil {
		t.Fatal(err)
	}

	// Modify migration file on disk after apply (drift)
	m1UpEdited := `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, email TEXT);`
	if err := os.WriteFile(filepath.Join(cfg.MigrationsDir, "20260822000001_users.up.sql"), []byte(m1UpEdited), 0o644); err != nil {
		t.Fatal(err)
	}

	report := evaluateTarget(cfg, "testdb")
	if report.Healthy {
		t.Fatal("evaluateTarget() healthy = true, want false (content hash drift)")
	}
	if report.Exit != 2 {
		t.Fatalf("evaluateTarget() exit = %d, want 2", report.Exit)
	}

	err = runDoctor("testdb")
	var exitErr *ExitCodeError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("runDoctor() err = %v, want exit code 2", err)
	}
}

func TestDoctorPendingMigration(t *testing.T) {
	dir, _, cfg := setupTestDoctorEnv(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Apply migration 1
	_, err = apply.Run(cfg, "testdb", "")
	if err != nil {
		t.Fatal(err)
	}

	// Add migration 2 without applying
	m2Up := `CREATE TABLE orders (id INTEGER PRIMARY KEY);`
	m2Down := `DROP TABLE orders;`
	if err := os.WriteFile(filepath.Join(cfg.MigrationsDir, "20260822000002_orders.up.sql"), []byte(m2Up), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.MigrationsDir, "20260822000002_orders.down.sql"), []byte(m2Down), 0o644); err != nil {
		t.Fatal(err)
	}

	report := evaluateTarget(cfg, "testdb")
	if report.Healthy {
		t.Fatal("evaluateTarget() healthy = true, want false (pending migration)")
	}
	if report.Exit != 2 {
		t.Fatalf("evaluateTarget() exit = %d, want 2", report.Exit)
	}

	err = runDoctor("testdb")
	var exitErr *ExitCodeError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("runDoctor() err = %v, want exit code 2", err)
	}
}

func TestDoctorDirtyLedger(t *testing.T) {
	dir, rawURL, cfg := setupTestDoctorEnv(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Apply migration 1
	_, err = apply.Run(cfg, "testdb", "")
	if err != nil {
		t.Fatal(err)
	}

	// Artificially mark dirty in schema_migrations
	eng := sqliteengine.SQLite{}
	db, err := eng.Open(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE schema_migrations SET dirty = 1`); err != nil {
		t.Fatal(err)
	}

	report := evaluateTarget(cfg, "testdb")
	if report.Healthy {
		t.Fatal("evaluateTarget() healthy = true, want false (dirty ledger)")
	}
	if report.Exit != 2 {
		t.Fatalf("evaluateTarget() exit = %d, want 2", report.Exit)
	}
}

func TestDoctorUnreachableOrUnknownTarget(t *testing.T) {
	dir, _, cfg := setupTestDoctorEnv(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	// Unknown target
	report := evaluateTarget(cfg, "nonexistent")
	if report.Exit != 1 {
		t.Fatalf("evaluateTarget(nonexistent) exit = %d, want 1", report.Exit)
	}

	// Target with unset env variable
	t.Setenv("DBTOOLS_TEST_DOCTOR_URL", "")
	report = evaluateTarget(cfg, "testdb")
	if report.Exit != 1 {
		t.Fatalf("evaluateTarget(unset env) exit = %d, want 1", report.Exit)
	}
}

func TestDoctorJSONOutput(t *testing.T) {
	dir, _, cfg := setupTestDoctorEnv(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	_, err = apply.Run(cfg, "testdb", "")
	if err != nil {
		t.Fatal(err)
	}

	jsonOutput = true
	t.Cleanup(func() { jsonOutput = false })

	report := evaluateTarget(cfg, "testdb")
	b, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}

	var parsed DoctorReport
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("Unmarshal DoctorReport failed: %v", err)
	}
	if parsed.Target != "testdb" || !parsed.Healthy || parsed.Exit != 0 || len(parsed.Checks) != 6 {
		t.Fatalf("parsed report mismatch: %+v", parsed)
	}
}

func TestDoctorLiveObjectDropDrift(t *testing.T) {
	dir, rawURL, cfg := setupTestDoctorEnv(t)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	_, err = apply.Run(cfg, "testdb", "")
	if err != nil {
		t.Fatal(err)
	}

	// Drop table directly out of band
	eng := sqliteengine.SQLite{}
	db, err := eng.Open(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`DROP TABLE users`); err != nil {
		t.Fatal(err)
	}

	report := evaluateTarget(cfg, "testdb")
	if report.Healthy {
		t.Fatal("evaluateTarget() healthy = true, want false (live drop drift)")
	}
	if report.Exit != 2 {
		t.Fatalf("evaluateTarget() exit = %d, want 2", report.Exit)
	}
}

func TestRenderDoctorHuman(t *testing.T) {
	rep := &DoctorReport{
		Target:  "prod",
		Engine:  "postgres",
		Healthy: false,
		Exit:    2,
		Checks: []CheckResult{
			{Name: "connectivity", Status: "ok", Message: "connected to postgres"},
			{Name: "drift-summary", Status: "fail", Message: "drift detected in 1 migration(s)"},
			{Name: "security-flags", Status: "warn", Message: "url_env missing"},
		},
	}
	out := renderDoctorHuman([]*DoctorReport{rep})
	if !strings.Contains(out, "Target: prod (postgres)") {
		t.Errorf("rendered output missing target header: %s", out)
	}
	if !strings.Contains(out, "[OK]    connectivity") {
		t.Errorf("rendered output missing [OK]: %s", out)
	}
	if !strings.Contains(out, "[FAIL]  drift-summary") {
		t.Errorf("rendered output missing [FAIL]: %s", out)
	}
	if !strings.Contains(out, "[WARN]  security-flags") {
		t.Errorf("rendered output missing [WARN]: %s", out)
	}
	if !strings.Contains(out, "Result: ISSUES DETECTED (exit 2)") {
		t.Errorf("rendered output missing verdict: %s", out)
	}
}
