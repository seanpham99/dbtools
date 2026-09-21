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

// TestUpAndStatusReportBelowWatermarkMigration is the regression test for
// issue #106: a migration file whose version sits below the target's
// watermark and was never applied can never be applied by `up` (which only
// steps forward). Before the fix the tool was indistinguishable from a
// correct one — `status` said "up to date", `up` exited 0, and the file was
// named nowhere.
//
// It walks a real SQLite target through the same states the verifier
// harness does: up (contiguous) -> status/[--json] clean -> a back-dated
// file added -> status/[--json] must name it -> up must exit 2 and print
// its path -> the file's table must still not exist -> plan must not call
// the target clean either.
func TestUpAndStatusReportBelowWatermarkMigration(t *testing.T) {
	dir := t.TempDir()
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
		jsonOutput = false
		upTarget, upURL, upDryRun = "local", "", false
		statusTarget, statusURL = "", ""
		planTarget, planURL = "", ""
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
	// A URL only ever reaches the target through the env var its config
	// names (golden rule 1).
	t.Setenv("DBTOOLS_BELOW_WATERMARK_TEST_URL", "sqlite://"+dbPath)
	cfgTOML := `migrations_dir = "migrations"

[migrations]
up_suffix = ".sql"

[targets.local]
url_env = "DBTOOLS_BELOW_WATERMARK_TEST_URL"
engine = "sqlite"
`
	if err := os.WriteFile("dbtools.toml", []byte(cfgTOML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Progress/refusal lines go through the logger (stderr in the real CLI);
	// capture them so the "it prints the ignored path" half of the contract
	// is asserted rather than assumed.
	var logs strings.Builder
	logger.SetOutput(&logs)
	t.Cleanup(func() { logger.SetOutput(os.Stderr) })

	upTarget, upURL, upDryRun = "local", "", false
	statusTarget, statusURL = "", ""
	planTarget, planURL = "", ""

	// --- 1. happy path: contiguous files, nothing ignored ---------------
	if err := runUp(); err != nil {
		t.Fatalf("runUp() with two contiguous migrations returned error: %v, want exit 0", err)
	}

	jsonOutput = true
	var runErr error
	out := captureStdout(t, func() { runErr = runStatus("local") })
	if runErr != nil {
		t.Fatalf("runStatus() on a clean target returned error: %v", runErr)
	}
	// The field's contract is "always present, [] when empty" — so the raw
	// document is checked, not just the decoded value.
	if !strings.Contains(out, `"ignored":[]`) {
		t.Fatalf("status --json = %s, want an always-present \"ignored\":[] on a clean target", out)
	}
	if strings.Contains(out, `"ignored":null`) {
		t.Fatalf("status --json = %s, want never null (docs/exit-codes.md JSON contract)", out)
	}
	var entries []struct {
		Target  string   `json:"target"`
		Pending []string `json:"pending"`
		Ignored []string `json:"ignored"`
	}
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatalf("status --json emitted %q, which does not parse: %v", out, err)
	}
	if len(entries) != 1 {
		t.Fatalf("status --json emitted %d entries, want 1: %s", len(entries), out)
	}
	if entries[0].Ignored == nil {
		t.Fatalf("status --json ignored decoded as nil, want []: %s", out)
	}
	if len(entries[0].Ignored) != 0 || len(entries[0].Pending) != 0 {
		t.Fatalf("clean target reported pending=%v ignored=%v, want both empty", entries[0].Pending, entries[0].Ignored)
	}

	jsonOutput = false
	out = captureStdout(t, func() { runErr = runStatus("local") })
	if runErr != nil {
		t.Fatalf("runStatus() (human) on a clean target returned error: %v", runErr)
	}
	if !strings.Contains(out, "up to date") {
		t.Fatalf("clean status = %q, want it to report up to date", out)
	}
	if strings.Contains(out, "ignored:") {
		t.Fatalf("clean status named an ignored file: %q", out)
	}

	// --- 2. the back-dated file ------------------------------------------
	writeMigration("20260102000000_b.sql", "CREATE TABLE b(i int);\n")

	out = captureStdout(t, func() { runErr = runStatus("local") })
	if runErr != nil {
		t.Fatalf("runStatus() in the below-watermark state returned error: %v", runErr)
	}
	// S2: the human output names the file.
	if !strings.Contains(out, "20260102000000_b.sql") {
		t.Fatalf("status = %q, want it to name the below-watermark file 20260102000000_b.sql", out)
	}
	if !strings.Contains(out, "below the watermark") {
		t.Fatalf("status = %q, want it to say the file is below the watermark", out)
	}

	jsonOutput = true
	ignoredJSON := captureStdout(t, func() { runErr = runStatus("local") })
	if runErr != nil {
		t.Fatalf("runStatus() --json in the below-watermark state returned error: %v", runErr)
	}
	// S3: the dedicated field carries the file, and it is not reported as
	// pending (that is exactly the trap: pending is empty here).
	if !strings.Contains(ignoredJSON, `"ignored":["migrations/20260102000000_b.sql"]`) {
		t.Fatalf("status --json = %s, want ignored to carry migrations/20260102000000_b.sql", ignoredJSON)
	}
	if !strings.Contains(ignoredJSON, `"pending":[]`) {
		t.Fatalf("status --json = %s, want pending to stay empty in this state", ignoredJSON)
	}
	jsonOutput = false

	// --- 3. up must not report a clean run -------------------------------
	logs.Reset()
	upErr := runUp()
	if upErr == nil {
		t.Fatal("runUp() in the below-watermark state returned nil, want exit code 2")
	}
	var exitErr *ExitCodeError
	if !errors.As(upErr, &exitErr) {
		t.Fatalf("runUp() error = %v (%T), want *ExitCodeError", upErr, upErr)
	}
	if exitErr.Code != 2 {
		t.Fatalf("runUp() exit code = %d, want 2 (drift/pending per docs/exit-codes.md)", exitErr.Code)
	}
	// S4: the ignored path is printed, not just implied by the exit code.
	if !strings.Contains(logs.String(), "migrations/20260102000000_b.sql") {
		t.Fatalf("up printed %q, want the ignored path migrations/20260102000000_b.sql", logs.String())
	}
	if !strings.Contains(logs.String(), "will never be applied") {
		t.Fatalf("up printed %q, want it to explain the file will never be applied", logs.String())
	}

	// The refusal must be a report, not an out-of-order apply.
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

	// --- 4. up --json: same refusal, ignored visible in the document -----
	jsonOutput = true
	var upJSONOut string
	upErr = nil
	upJSONOut = captureStdout(t, func() { upErr = runUp() })
	if !strings.Contains(upJSONOut, `"ignored":["migrations/20260102000000_b.sql"]`) {
		t.Fatalf("up --json = %s, want the ignored field to carry the path", upJSONOut)
	}
	if !errors.As(upErr, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("up --json error = %v, want exit code 2", upErr)
	}
	jsonOutput = false

	// --- 5. plan: the preview surface must not call this target clean ----
	planTarget = "local"
	planJSON := captureStdout(t, func() { runErr = runPlan() })
	if runErr == nil {
		t.Fatal("runPlan() in the below-watermark state returned nil, want exit code 2")
	}
	if !errors.As(runErr, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("runPlan() error = %v, want exit code 2", runErr)
	}
	if !strings.Contains(planJSON, `"ignored":["migrations/20260102000000_b.sql"]`) {
		t.Fatalf("plan --json = %s, want the ignored field to carry the path", planJSON)
	}
}
