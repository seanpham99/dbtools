package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanpham99/dbtools/internal/engine/sqliteengine"
	"github.com/seanpham99/dbtools/internal/logger"
)

// TestPushAndDryRunReportBelowWatermarkMigration covers the surfaces the first
// pass left silent for the state in #106: `push --json` after a successful
// apply, and both dry-run paths (`up --dry-run`, `push --dry-run`).
//
// Each of them can finish with exit 0 — the apply genuinely ran, the preview
// genuinely found nothing above the watermark — while a file below the
// watermark stays permanently unapplied. That is the same "indistinguishable
// from correct" failure the issue reports, reached through different commands,
// so each one has to name the file and fail the run.
func TestPushAndDryRunReportBelowWatermarkMigration(t *testing.T) {
	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
		jsonOutput = false
		upTarget, upURL, upDryRun = "local", "", false
		pushURL, pushDryRun, pushYes = "", false, false
	})
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll("migrations", 0o755); err != nil {
		t.Fatal(err)
	}

	writeMigration := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join("migrations", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeMigration("20260101000000_a.sql", "CREATE TABLE a(i int);\n")
	writeMigration("20260103000000_c.sql", "CREATE TABLE c(i int);\n")

	dbPath := filepath.Join(dir, "dev.db")
	// A URL reaches a target only through the env var its config names
	// (dbtools AGENTS.md golden rule 1).
	t.Setenv("DBTOOLS_IGNORED_DRYRUN_TEST_URL", "sqlite://"+dbPath)
	cfgTOML := `migrations_dir = "migrations"

[migrations]
up_suffix = ".sql"

[targets.local]
url_env = "DBTOOLS_IGNORED_DRYRUN_TEST_URL"
engine = "sqlite"
`
	if err := os.WriteFile("dbtools.toml", []byte(cfgTOML), 0o644); err != nil {
		t.Fatal(err)
	}

	var logs strings.Builder
	logger.SetOutput(&logs)
	t.Cleanup(func() { logger.SetOutput(os.Stderr) })

	upTarget, upURL, upDryRun = "local", "", false
	if err := runUp(); err != nil {
		t.Fatalf("initial up over two contiguous migrations: %v", err)
	}

	// The back-dated file: below the watermark, no applied ledger row.
	writeMigration("20260102000000_b.sql", "CREATE TABLE b(i int);\n")
	const ignoredPath = "migrations/20260102000000_b.sql"

	var exitErr *ExitCodeError

	// --- 1. up --dry-run with nothing pending must not read as up to date --
	logs.Reset()
	upDryRun = true
	var dryErr error
	dryOut := captureStdout(t, func() { dryErr = runUp() })
	if !errors.As(dryErr, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("up --dry-run error = %v, want exit code 2", dryErr)
	}
	if strings.Contains(dryOut, "already up to date") {
		t.Fatalf("up --dry-run stdout = %q, want it not to call the target up to date", dryOut)
	}
	if !strings.Contains(logs.String(), ignoredPath) {
		t.Fatalf("up --dry-run logs = %q, want the ignored path %s", logs.String(), ignoredPath)
	}

	// --- 2. up --dry-run --json: field present, never null -----------------
	logs.Reset()
	jsonOutput = true
	dryErr = nil
	dryJSON := captureStdout(t, func() { dryErr = runUp() })
	if !strings.Contains(dryJSON, `"ignored":["migrations/20260102000000_b.sql"]`) {
		t.Fatalf("up --dry-run --json = %s, want the ignored path in the document", dryJSON)
	}
	if !errors.As(dryErr, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("up --dry-run --json error = %v, want exit code 2", dryErr)
	}
	var parsed struct {
		Pending []json.RawMessage `json:"pending"`
		Ignored []string          `json:"ignored"`
	}
	if err := json.Unmarshal([]byte(dryJSON), &parsed); err != nil {
		t.Fatalf("up --dry-run --json = %q, which does not parse: %v", dryJSON, err)
	}
	if parsed.Ignored == nil {
		t.Fatalf("up --dry-run --json ignored decoded as nil, want []: %s", dryJSON)
	}
	jsonOutput = false
	upDryRun = false

	// --- 3. push --dry-run reports the same state ---------------------------
	logs.Reset()
	pushDryRun = true
	pushErr := runPush("local")
	if !errors.As(pushErr, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("push --dry-run error = %v, want exit code 2", pushErr)
	}
	if !strings.Contains(logs.String(), ignoredPath) {
		t.Fatalf("push --dry-run logs = %q, want the ignored path %s", logs.String(), ignoredPath)
	}
	pushDryRun = false

	// --- 4. a real push that applies new work still reports what it left ---
	writeMigration("20260104000000_d.sql", "CREATE TABLE d(i int);\n")
	jsonOutput = true
	pushYes = true
	pushErr = nil
	pushJSON := captureStdout(t, func() { pushErr = runPush("local") })
	if !errors.As(pushErr, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("push --json error = %v, want exit code 2 while %s stays unapplied", pushErr, ignoredPath)
	}
	if !strings.Contains(pushJSON, `"ignored":["migrations/20260102000000_b.sql"]`) {
		t.Fatalf("push --json = %s, want the ignored path in the document", pushJSON)
	}
	if !strings.Contains(pushJSON, `"pending":[]`) {
		t.Fatalf("push --json = %s, want the applied version gone from pending", pushJSON)
	}
	jsonOutput = false
	pushYes = false

	// --- 5. the refusal is a report: nothing was applied out of order ------
	eng := sqliteengine.SQLite{}
	db, err := eng.Open("sqlite://" + dbPath)
	if err != nil {
		t.Fatalf("opening %s: %v", dbPath, err)
	}
	defer db.Close()
	var bTables int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'b'`).Scan(&bTables); err != nil {
		t.Fatalf("introspecting the target: %v", err)
	}
	if bTables != 0 {
		t.Fatal("table b exists: the below-watermark migration was applied out of order")
	}
}
