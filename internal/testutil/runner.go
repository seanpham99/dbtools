package testutil

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/apply"
	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/down"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/generate"
	"github.com/seanpham99/dbtools/internal/testdb"
	"github.com/seanpham99/dbtools/internal/verify"
)

func findRepoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}

// RunAssets runs the full classicmodels asset suite against rawURL for the given dialect:
// 1. Applies all migrations in sequence (E1)
// 2. Verifies clean ledger + hash state (E2)
// 3. Runs seed.sql with optional scale multiplier (E3, scale tier)
// 4. Executes data integrity queries (composite PK, self-referential FK, decimals, NULLs)
// 5. Validates generate output against committed goldens (or updates with DBTOOLS_TEST_UPDATE=1)
// 6. Runs edge-case matrix (E4 hash drift, E5 object drop drift, E6 down reversal, E10 re-apply)
func RunAssets(t *testing.T, dialect, rawURL string) {
	t.Helper()

	if strings.HasPrefix(rawURL, "sqlite://") {
		p := strings.TrimPrefix(rawURL, "sqlite://")
		if p != "" && p != ":memory:" {
			_ = os.Remove(p)
			_ = os.Remove(p + "-wal")
			_ = os.Remove(p + "-shm")
		}
	}

	if err := testdb.ResetTracking(rawURL); err != nil {
		t.Fatalf("ResetTracking failed: %v", err)
	}

	migrationsDir := t.TempDir()
	if err := ExtractMigrations(migrationsDir, dialect); err != nil {
		t.Fatalf("ExtractMigrations(%s) failed: %v", dialect, err)
	}

	eng, err := engine.ForTarget(dialect, rawURL)
	if err != nil {
		t.Fatalf("engine.ForTarget(%s) failed: %v", dialect, err)
	}

	db, err := eng.Open(rawURL)
	if err != nil {
		t.Fatalf("eng.Open failed: %v", err)
	}
	defer db.Close()

	// Clean up any existing fixture tables so we start fresh
	tablesToDrop := []string{"payments", "orderdetails", "orders", "customers", "products", "productlines", "employees", "offices"}
	for _, tbl := range tablesToDrop {
		if dialect == "postgres" {
			_, _ = db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", tbl))
		} else if dialect == "mssql" {
			_, _ = db.Exec(fmt.Sprintf("IF OBJECT_ID('dbo.%s', 'U') IS NOT NULL DROP TABLE dbo.%s", tbl, tbl))
		} else {
			_, _ = db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", tbl))
		}
	}

	cfg := &config.Config{
		MigrationsDir: migrationsDir,
		Targets: map[string]config.Target{
			"test-target": {URLEnv: "DBTOOLS_TEST_URL"},
		},
	}
	t.Setenv("DBTOOLS_TEST_URL", rawURL)

	t.Cleanup(func() {
		for _, tbl := range tablesToDrop {
			if dialect == "postgres" {
				_, _ = db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", tbl))
			} else if dialect == "mssql" {
				_, _ = db.Exec(fmt.Sprintf("IF OBJECT_ID('dbo.%s', 'U') IS NOT NULL DROP TABLE dbo.%s", tbl, tbl))
			} else {
				_, _ = db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", tbl))
			}
		}
		_ = testdb.ResetTracking(rawURL)
	})

	// --- E1: Apply all migrations in sequence via apply.Run ---
	status, err := apply.Run(cfg, "test-target", "")
	if err != nil {
		t.Fatalf("apply.Run failed: %v", err)
	}
	if status.CurrentVersion != 20260822000004 {
		t.Fatalf("CurrentVersion = %d, want 20260822000004", status.CurrentVersion)
	}

	// --- E2: Verify clean ledger state ---
	report, err := verify.Collect(db, eng, migrationsDir, "test-target")
	if err != nil {
		t.Fatalf("verify.Collect failed: %v", err)
	}
	if len(report.Entries) != 4 {
		t.Fatalf("verify entries count = %d, want 4", len(report.Entries))
	}
	for _, e := range report.Entries {
		if e.Status != "OK" {
			t.Errorf("verify entry v%d = %s (%s), want OK", e.Version, e.Status, e.Detail)
		}
	}

	// --- E3: Seed data & Scale tier ---
	seedSQL, err := GetSeedSQL()
	if err != nil {
		t.Fatalf("GetSeedSQL failed: %v", err)
	}

	// Execute seed SQL
	if err := executeSQLStatements(db, seedSQL); err != nil {
		t.Fatalf("executing seed.sql failed: %v", err)
	}

	// Scale tier: if DBTOOLS_TEST_SCALE=1, inflate row counts
	if os.Getenv("DBTOOLS_TEST_SCALE") == "1" {
		for i := 1; i <= 5; i++ {
			scaledCheck := fmt.Sprintf("CHK_SCALE_%d", i)
			ins := fmt.Sprintf("INSERT INTO payments (customerNumber, checkNumber, paymentDate, amount) VALUES (103, '%s', '2024-03-01', 100.50)", scaledCheck)
			if _, err := db.Exec(ins); err != nil {
				t.Fatalf("scale insert failed: %v", err)
			}
		}
	}

	// --- 4. Query Checks: composite PK, self-referential FK, decimals, NULLs ---
	// 4a. Self-referential FK hierarchy
	var bossJob string
	err = db.QueryRow(`
		SELECT e2.jobTitle 
		FROM employees e1 
		JOIN employees e2 ON e1.reportsTo = e2.employeeNumber 
		WHERE e1.employeeNumber = 1056
	`).Scan(&bossJob)
	if err != nil || bossJob != "President" {
		t.Errorf("employee 1056 manager job = %q (err=%v), want President", bossJob, err)
	}

	// 4b. Composite PK on orderdetails (orderNumber + productCode)
	var lineCount int
	err = db.QueryRow(`SELECT COUNT(*) FROM orderdetails WHERE orderNumber = 10100`).Scan(&lineCount)
	if err != nil || lineCount != 3 {
		t.Errorf("order 10100 orderdetails count = %d (err=%v), want 3", lineCount, err)
	}

	// 4c. Composite PK on payments (customerNumber + checkNumber)
	var paymentAmount float64
	err = db.QueryRow(`SELECT amount FROM payments WHERE customerNumber = 103 AND checkNumber = 'HQ336336'`).Scan(&paymentAmount)
	if err != nil || paymentAmount < 14571.0 {
		t.Errorf("payment amount for 103/HQ336336 = %f (err=%v), want ~14571.44", paymentAmount, err)
	}

	// 4d. NULL handling on addressLine2 & shippedDate
	var nullAddrCount int
	err = db.QueryRow(`SELECT COUNT(*) FROM customers WHERE addressLine2 IS NULL`).Scan(&nullAddrCount)
	if err != nil || nullAddrCount < 4 {
		t.Errorf("customers with NULL addressLine2 = %d (err=%v), want >= 4", nullAddrCount, err)
	}

	// --- 5. Golden Typegen (generate models.py and models.ts) ---
	tables, _, err := eng.Introspect(db, []string{"dbtools_migration_history", "schema_migrations"})
	if err != nil {
		t.Fatalf("Introspect failed: %v", err)
	}
	if len(tables) < 8 {
		t.Fatalf("Introspect tables = %d, want at least 8 classicmodels tables", len(tables))
	}

	pyOutput, err := generate.Render(tables, "test-target")
	if err != nil {
		t.Fatalf("generate.Render failed: %v", err)
	}
	tsOutput, err := generate.RenderTS(tables, "test-target", true)
	if err != nil {
		t.Fatalf("generate.RenderTS failed: %v", err)
	}

	repoRoot := findRepoRoot()
	goldenDir := filepath.Join(repoRoot, "internal", "testutil", "testdata", "golden", dialect)
	if os.Getenv("DBTOOLS_TEST_UPDATE") == "1" {
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatalf("creating golden dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(goldenDir, "models.py"), []byte(pyOutput), 0o644); err != nil {
			t.Fatalf("writing golden models.py: %v", err)
		}
		if err := os.WriteFile(filepath.Join(goldenDir, "models.ts"), []byte(tsOutput), 0o644); err != nil {
			t.Fatalf("writing golden models.ts: %v", err)
		}
	} else {
		goldenPy, err := ReadGolden(dialect, "python")
		if err != nil {
			t.Fatalf("ReadGolden(%s, python) failed: %v (run with DBTOOLS_TEST_UPDATE=1 to generate)", dialect, err)
		}
		goldenTS, err := ReadGolden(dialect, "ts")
		if err != nil {
			t.Fatalf("ReadGolden(%s, ts) failed: %v (run with DBTOOLS_TEST_UPDATE=1 to generate)", dialect, err)
		}

		if StripGeneratedHeader(pyOutput) != StripGeneratedHeader(goldenPy) {
			t.Errorf("generated python models differ from golden for %s.\nACTUAL:\n%s\nEXPECTED:\n%s", dialect, StripGeneratedHeader(pyOutput), StripGeneratedHeader(goldenPy))
		}
		if StripGeneratedHeader(tsOutput) != StripGeneratedHeader(goldenTS) {
			t.Errorf("generated ts models differ from golden for %s.\nACTUAL:\n%s\nEXPECTED:\n%s", dialect, StripGeneratedHeader(tsOutput), StripGeneratedHeader(goldenTS))
		}
	}

	// --- E4: Content-Hash Drift Check ---
	mig1Path := filepath.Join(migrationsDir, "20260822000001_offices_employees.up.sql")
	origMig1, err := os.ReadFile(mig1Path)
	if err != nil {
		t.Fatal(err)
	}
	// Append a comment to introduce content hash drift
	if err := os.WriteFile(mig1Path, append(origMig1, []byte("\n-- edited after apply")...), 0o644); err != nil {
		t.Fatal(err)
	}
	driftReport, err := verify.Collect(db, eng, migrationsDir, "test-target")
	if err != nil {
		t.Fatalf("verify after edit returned err: %v", err)
	}
	var foundHashDrift bool
	for _, e := range driftReport.Entries {
		if e.Version == 20260822000001 && e.Status == "DRIFT" && strings.Contains(e.Detail, "content hash") {
			foundHashDrift = true
		}
	}
	if !foundHashDrift {
		t.Errorf("expected content hash drift on v1 after edit, got: %+v", driftReport.Entries)
	}
	// Restore original file
	if err := os.WriteFile(mig1Path, origMig1, 0o644); err != nil {
		t.Fatal(err)
	}

	// --- E5: Out-of-band object drop drift ---
	if dialect == "postgres" {
		_, _ = db.Exec("DROP TABLE payments CASCADE")
	} else if dialect == "mssql" {
		_, _ = db.Exec("DROP TABLE payments")
	} else {
		_, _ = db.Exec("DROP TABLE payments")
	}
	dropReport, err := verify.Collect(db, eng, migrationsDir, "test-target")
	if err != nil {
		t.Fatalf("verify after drop returned err: %v", err)
	}
	var foundDropDrift bool
	for _, e := range dropReport.Entries {
		if e.Version == 20260822000004 && e.Status == "DRIFT" && strings.Contains(e.Detail, "payments") {
			foundDropDrift = true
		}
	}
	if !foundDropDrift {
		t.Errorf("expected missing table drift on v4 after dropping payments, got: %+v", dropReport.Entries)
	}

	// Recreate table and seed for down test
	recreateV4, err := os.ReadFile(filepath.Join(migrationsDir, "20260822000004_orders_payments.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	_ = executeSQLStatements(db, string(recreateV4))

	// --- E6: Down migrations reversal ---
	downRes, err := down.Run(cfg, "test-target", 1, "")
	if err != nil {
		t.Fatalf("down.Run failed: %v", err)
	}
	if len(downRes.RevertedVersions) != 1 || downRes.RevertedVersions[0] != 20260822000004 || downRes.CurrentVersion != 20260822000003 {
		t.Errorf("down.Run result = %+v, want RevertedVersions=[20260822000004], CurrentVersion=20260822000003", downRes)
	}

	// Verify ledger marks v4 reverted, not drift
	downVerify, err := verify.Collect(db, eng, migrationsDir, "test-target")
	if err != nil {
		t.Fatalf("verify after down failed: %v", err)
	}
	for _, e := range downVerify.Entries {
		if e.Status != "OK" {
			t.Errorf("verify entry after down v%d = %s (%s), want OK", e.Version, e.Status, e.Detail)
		}
	}

	// --- E10: Re-apply migration ---
	reapplyStatus, err := apply.Run(cfg, "test-target", "")
	if err != nil {
		t.Fatalf("apply.Run for re-apply failed: %v", err)
	}
	if reapplyStatus.CurrentVersion != 20260822000004 {
		t.Fatalf("reapplyStatus.CurrentVersion = %d, want 20260822000004", reapplyStatus.CurrentVersion)
	}

	finalVerify, err := verify.Collect(db, eng, migrationsDir, "test-target")
	if err != nil {
		t.Fatalf("final verify failed: %v", err)
	}
	for _, e := range finalVerify.Entries {
		if e.Status != "OK" {
			t.Errorf("final verify entry v%d = %s (%s), want OK", e.Version, e.Status, e.Detail)
		}
	}
}

func executeSQLStatements(db *sql.DB, sqlContent string) error {
	lines := strings.Split(sqlContent, "\n")
	var currentBatch []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.EqualFold(trimmed, "GO") {
			if len(currentBatch) > 0 {
				stmt := strings.Join(currentBatch, "\n")
				if strings.TrimSpace(stmt) != "" {
					if _, err := db.Exec(stmt); err != nil {
						return fmt.Errorf("executing batch %q: %w", stmt, err)
					}
				}
				currentBatch = nil
			}
			continue
		}
		currentBatch = append(currentBatch, line)
	}

	if len(currentBatch) > 0 {
		stmt := strings.Join(currentBatch, "\n")
		if strings.TrimSpace(stmt) != "" {
			if _, err := db.Exec(stmt); err != nil {
				return fmt.Errorf("executing batch %q: %w", stmt, err)
			}
		}
	}
	return nil
}
