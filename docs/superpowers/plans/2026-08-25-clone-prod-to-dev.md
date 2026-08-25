# Clone (prod→dev) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `dbtools clone <source> <dest>` — copy schema-compatible data from `source` into `dest`, masking sensitive columns by default, so a developer can refresh a local/dev database from a snapshot of prod without hand-rolling a dump/restore/scrub script.

**Architecture:** A new `internal/clone` package with a small set of pure, dialect-aware SQL-building/masking functions (fully unit-testable without a database) plus one DB-touching `Run` orchestrator, following the same shape as `internal/apply.Run` — one function, no exported types beyond the request/response shape. A new `cmd/clone.go` wires it into Cobra with the same safety-gate conventions `cmd/reset.go`/`cmd/down.go` already establish (`--yes` required, `requireUnprotected` on the target being written to).

**Tech Stack:** Go 1.25, `database/sql` (generic — no new driver dependency; clone works through whichever `engine.Engine` the two targets already resolve to).

## Global Constraints

- Go 1.25+, matches `go.mod`.
- Never hardcode a connection string or credential — unchanged from the rest of the codebase; tests use `t.Setenv`.
- **Clone requires source and dest to use the same database engine.** Cross-dialect data-type/placeholder translation is a different, much larger problem (see Task 2's `placeholder` design note) and is explicitly out of scope. This matches the roadmap's own framing — one project cloning its own prod into its own dev, not a migration between engines.
- **Dest must not be `protected`.** Clone always deletes and repopulates every cloned table in `dest` — this is exactly as destructive as `reset`, so it reuses `cmd/openTarget.go`'s `requireUnprotected` with no override, the same rule `reset`/`repair`/`force` use (not `push`'s softer "`protected` + `--yes` is allowed" rule — clone flows FROM prod INTO dev, never the reverse, and accidentally reversing the two arguments must not be recoverable by just adding `--yes`).
- **`--yes` is mandatory** regardless of whether `dest` is protected (mirrors `reset`, which requires `--yes` even for the unprotected default `local` target — the operation is destructive either way).
- Masking is **on by default**; `--no-mask` is the explicit, documented opt-out. `--mask` also exists (per the roadmap's literal `[--mask|--no-mask]` flag spec) as a no-op that only serves to conflict with `--no-mask` if both are passed.
- Reuse `cfg.Generate.Exclude` (already defaults to `["dbtools_migration_history", "schema_migrations"]` — see `internal/config/config.go`'s `Load`) unioned with a new `cfg.Clone.Exclude` — dbtools's own bookkeeping tables must never be cloned, and this is already solved once for `generate`; don't re-solve it.
- Follow `internal/apply`'s shape: one package, one `Run` entrypoint, a `Result` struct returned as `--json` output — not a stack of exported types.

---

### Task 1: `dbtools.toml` config schema — `[clone]` and `[clone.mask]`

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Interfaces:**
- Produces: `type CloneConfig struct { Exclude []string; Mask map[string]string }` and a `Clone CloneConfig` field on `Config`, parsed from a `[clone]` TOML table (`exclude = [...]`) and a `[clone.mask]` sub-table (`column_name = "strategy"`). Used by Task 3's `clone.Run`.

- [ ] **Step 1: Write the failing test**

```go
func TestLoad_ParsesCloneConfig(t *testing.T) {
	path := writeTemp(t, `
[targets.local]
url_env = "L_URL"

[clone]
exclude = ["audit_log"]

[clone.mask]
email = "email"
phone = "redact"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if len(cfg.Clone.Exclude) != 1 || cfg.Clone.Exclude[0] != "audit_log" {
		t.Errorf("Clone.Exclude = %v, want [audit_log]", cfg.Clone.Exclude)
	}
	if cfg.Clone.Mask["email"] != "email" || cfg.Clone.Mask["phone"] != "redact" {
		t.Errorf("Clone.Mask = %v, want email->email, phone->redact", cfg.Clone.Mask)
	}
}

func TestLoad_CloneConfigDefaultsToEmpty(t *testing.T) {
	path := writeTemp(t, `
[targets.local]
url_env = "L_URL"
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if len(cfg.Clone.Exclude) != 0 || len(cfg.Clone.Mask) != 0 {
		t.Errorf("Clone = %+v, want zero-value when [clone] is absent", cfg.Clone)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -run TestLoad_ParsesCloneConfig -v`
Expected: FAIL — `cfg.Clone` doesn't compile (no such field).

- [ ] **Step 3: Add the field**

In `internal/config/config.go`, alongside the existing `GenerateConfig`:

```go
// CloneConfig holds settings for `dbtools clone`.
type CloneConfig struct {
	// Exclude lists additional tables never to clone, unioned with
	// Generate.Exclude (which already protects dbtools's own bookkeeping
	// tables — see Load's defaulting below).
	Exclude []string `toml:"exclude"`
	// Mask maps a column name (case-insensitive) to a masking strategy:
	// "redact", "email", or "hash". Columns not listed here but matching
	// a small built-in sensitive-name list (email, phone, ssn, password)
	// are masked with a default strategy unless --no-mask is passed —
	// see internal/clone's maskPlanFor.
	Mask map[string]string `toml:"mask"`
}
```

Add the field to `Config`:

```go
type Config struct {
	MigrationsDir string            `toml:"migrations_dir"`
	Targets       map[string]Target `toml:"targets"`
	Generate      GenerateConfig    `toml:"generate"`
	Clone         CloneConfig       `toml:"clone"`
}
```

No defaulting logic needed in `Load` — a nil `Exclude`/`Mask` behaves correctly everywhere they're used (an empty slice/map to range over or union).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -run 'TestLoad_ParsesCloneConfig|TestLoad_CloneConfigDefaultsToEmpty' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "feat(config): add [clone] and [clone.mask] to dbtools.toml"
```

---

### Task 2: Pure clone logic — masking, placeholders, SQL building

**Files:**
- Create: `internal/clone/mask.go`
- Create: `internal/clone/mask_test.go`
- Create: `internal/clone/sql.go`
- Create: `internal/clone/sql_test.go`

**Interfaces:**
- Consumes: nothing outside the standard library.
- Produces:
  - `type MaskStrategy string` with constants `MaskRedact`, `MaskEmail`, `MaskHash`.
  - `func maskPlanFor(colNames []string, configured map[string]string) map[string]MaskStrategy`
  - `func applyMask(strategy MaskStrategy, value any, counters map[string]int, colName string) any`
  - `func placeholder(engineName string, i int) string`
  - `func buildSelectSQL(engineName, table string, limit int, where string) string`
  - `func buildInsertSQL(engineName, table string, columns []string) string`

  All six are used by Task 3's `copyTable`.

- [ ] **Step 1: Write the failing masking tests**

```go
package clone

import "testing"

func TestMaskPlanFor_BuiltinSensitiveColumns(t *testing.T) {
	plan := maskPlanFor([]string{"id", "email", "phone", "ssn", "password", "notes"}, nil)
	want := map[string]MaskStrategy{
		"email":    MaskEmail,
		"phone":    MaskRedact,
		"ssn":      MaskRedact,
		"password": MaskRedact,
	}
	if len(plan) != len(want) {
		t.Fatalf("maskPlanFor() = %v, want %v", plan, want)
	}
	for col, strat := range want {
		if plan[col] != strat {
			t.Errorf("plan[%q] = %q, want %q", col, plan[col], strat)
		}
	}
}

func TestMaskPlanFor_ConfiguredOverridesBuiltin(t *testing.T) {
	plan := maskPlanFor([]string{"email"}, map[string]string{"email": "hash"})
	if plan["email"] != MaskHash {
		t.Errorf("plan[email] = %q, want hash (config must win over the email built-in)", plan["email"])
	}
}

func TestMaskPlanFor_ConfiguredAddsNonBuiltinColumn(t *testing.T) {
	plan := maskPlanFor([]string{"customerName"}, map[string]string{"customerName": "redact"})
	if plan["customerName"] != MaskRedact {
		t.Errorf("plan[customerName] = %q, want redact", plan["customerName"])
	}
}

func TestMaskPlanFor_CaseInsensitiveConfigKey(t *testing.T) {
	plan := maskPlanFor([]string{"Email"}, map[string]string{"email": "hash"})
	if plan["Email"] != MaskHash {
		t.Errorf("plan[Email] = %q, want hash (config keys match case-insensitively)", plan["Email"])
	}
}

func TestApplyMask_NilPassesThrough(t *testing.T) {
	if got := applyMask(MaskRedact, nil, map[string]int{}, "email"); got != nil {
		t.Errorf("applyMask(nil) = %v, want nil (never invent data for a real NULL)", got)
	}
}

func TestApplyMask_RedactString(t *testing.T) {
	if got := applyMask(MaskRedact, "real@example.com", map[string]int{}, "email"); got != "[REDACTED]" {
		t.Errorf("applyMask(redact, string) = %v, want [REDACTED]", got)
	}
}

func TestApplyMask_RedactBytes(t *testing.T) {
	got := applyMask(MaskRedact, []byte("secret"), map[string]int{}, "notes")
	b, ok := got.([]byte)
	if !ok || string(b) != "[REDACTED]" {
		t.Errorf("applyMask(redact, []byte) = %v, want []byte(\"[REDACTED]\")", got)
	}
}

func TestApplyMask_RedactNonStringPassesThrough(t *testing.T) {
	// Redacting a number has no safe generic representation — a
	// misconfigured mask on e.g. a credit_limit column must not corrupt
	// the row's type (which would break the INSERT), so it passes through.
	if got := applyMask(MaskRedact, int64(42), map[string]int{}, "credit_limit"); got != int64(42) {
		t.Errorf("applyMask(redact, int64) = %v, want unchanged 42", got)
	}
}

func TestApplyMask_EmailIsDeterministicallyUniquePerRow(t *testing.T) {
	counters := map[string]int{}
	first := applyMask(MaskEmail, "a@example.com", counters, "email")
	second := applyMask(MaskEmail, "b@example.com", counters, "email")
	if first == second {
		t.Fatalf("applyMask(email) produced the same value twice: %v", first)
	}
	if first != "user1@example.invalid" || second != "user2@example.invalid" {
		t.Errorf("applyMask(email) = %v, %v; want user1@example.invalid, user2@example.invalid", first, second)
	}
}

func TestApplyMask_HashIsDeterministicAndTwelveChars(t *testing.T) {
	counters := map[string]int{}
	got1 := applyMask(MaskHash, "same-value", counters, "external_id")
	got2 := applyMask(MaskHash, "same-value", counters, "external_id")
	if got1 != got2 {
		t.Errorf("applyMask(hash) = %v then %v, want identical (deterministic)", got1, got2)
	}
	s, ok := got1.(string)
	if !ok || len(s) != 12 {
		t.Errorf("applyMask(hash) = %v, want a 12-char hex string", got1)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/clone/... -run 'TestMaskPlanFor|TestApplyMask' -v`
Expected: FAIL — package doesn't exist yet.

- [ ] **Step 3: Write `mask.go`**

```go
// Package clone copies data from one dbtools target to another of the
// same engine, masking sensitive columns by default. See
// docs/superpowers/plans/2026-08-25-clone-prod-to-dev.md for the design.
package clone

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// MaskStrategy names how one column's values are transformed during clone.
type MaskStrategy string

const (
	// MaskRedact replaces string/[]byte values with a fixed placeholder.
	// Non-string values pass through unchanged (there is no safe generic
	// redaction for a number without knowing its semantics).
	MaskRedact MaskStrategy = "redact"
	// MaskEmail replaces a value with a deterministically unique synthetic
	// address (userN@example.invalid), preserving uniqueness for any
	// unique constraint on the column.
	MaskEmail MaskStrategy = "email"
	// MaskHash replaces a value with a 12-hex-char SHA-256 prefix of its
	// string representation — deterministic (the same input always maps
	// to the same output, useful when the value is referenced elsewhere)
	// but non-reversible.
	MaskHash MaskStrategy = "hash"
)

// builtinSensitiveColumns is the default-deny list: these column names
// (case-insensitive, exact match) are masked even with no [clone.mask]
// entry, unless --no-mask is passed. This is deliberately small and
// literal — dbtools has no way to classify arbitrary schemas, so it only
// protects the columns experience says show up by these exact names.
var builtinSensitiveColumns = map[string]MaskStrategy{
	"email":    MaskEmail,
	"phone":    MaskRedact,
	"ssn":      MaskRedact,
	"password": MaskRedact,
}

// maskPlanFor builds the column -> strategy plan for one table's columns.
// An explicit [clone.mask] entry always wins over a built-in default;
// configured strategy names are used verbatim (an unrecognized strategy
// name is handled by applyMask, which passes the value through unchanged
// for any strategy it doesn't recognize).
func maskPlanFor(colNames []string, configured map[string]string) map[string]MaskStrategy {
	plan := make(map[string]MaskStrategy)
	for _, name := range colNames {
		lower := strings.ToLower(name)
		if strat, ok := configured[lower]; ok {
			plan[name] = MaskStrategy(strat)
			continue
		}
		if strat, ok := builtinSensitiveColumns[lower]; ok {
			plan[name] = strat
		}
	}
	return plan
}

// applyMask transforms value according to strategy. A nil value (a real
// SQL NULL) always passes through unchanged — masking never invents data
// for a column that was genuinely absent. counters holds per-column
// running state (used by MaskEmail to number synthetic addresses); it is
// shared across every call for one table's clone and must be created
// fresh per table.
func applyMask(strategy MaskStrategy, value any, counters map[string]int, colName string) any {
	if value == nil {
		return nil
	}
	switch strategy {
	case MaskRedact:
		switch value.(type) {
		case string:
			return "[REDACTED]"
		case []byte:
			return []byte("[REDACTED]")
		default:
			return value
		}
	case MaskEmail:
		counters[colName]++
		return fmt.Sprintf("user%d@example.invalid", counters[colName])
	case MaskHash:
		s := fmt.Sprintf("%v", value)
		sum := sha256.Sum256([]byte(s))
		return hex.EncodeToString(sum[:])[:12]
	default:
		return value
	}
}
```

- [ ] **Step 4: Run masking tests to verify they pass**

Run: `go test ./internal/clone/... -run 'TestMaskPlanFor|TestApplyMask' -v`
Expected: PASS.

- [ ] **Step 5: Write the failing SQL-building tests**

```go
package clone

import "testing"

func TestPlaceholder(t *testing.T) {
	cases := []struct {
		engineName string
		i          int
		want       string
	}{
		{"mssql", 1, "@p1"},
		{"mssql", 3, "@p3"},
		{"postgres", 1, "$1"},
		{"postgres", 2, "$2"},
		{"sqlite", 1, "?"},
		{"mysql", 1, "?"},
	}
	for _, c := range cases {
		if got := placeholder(c.engineName, c.i); got != c.want {
			t.Errorf("placeholder(%q, %d) = %q, want %q", c.engineName, c.i, got, c.want)
		}
	}
}

func TestBuildSelectSQL_NoLimitNoWhere(t *testing.T) {
	got := buildSelectSQL("postgres", "customers", 0, "")
	want := "SELECT * FROM customers"
	if got != want {
		t.Errorf("buildSelectSQL() = %q, want %q", got, want)
	}
}

func TestBuildSelectSQL_LimitOnNonMSSQL(t *testing.T) {
	got := buildSelectSQL("sqlite", "customers", 10, "")
	want := "SELECT * FROM customers LIMIT 10"
	if got != want {
		t.Errorf("buildSelectSQL() = %q, want %q", got, want)
	}
}

func TestBuildSelectSQL_LimitOnMSSQLUsesTOP(t *testing.T) {
	got := buildSelectSQL("mssql", "customers", 10, "")
	want := "SELECT TOP 10 * FROM customers"
	if got != want {
		t.Errorf("buildSelectSQL() = %q, want %q", got, want)
	}
}

func TestBuildSelectSQL_Where(t *testing.T) {
	got := buildSelectSQL("postgres", "orders", 0, "status = 'Shipped'")
	want := "SELECT * FROM orders WHERE status = 'Shipped'"
	if got != want {
		t.Errorf("buildSelectSQL() = %q, want %q", got, want)
	}
}

func TestBuildSelectSQL_WhereAndLimitMSSQL(t *testing.T) {
	got := buildSelectSQL("mssql", "orders", 5, "status = 'Shipped'")
	want := "SELECT TOP 5 * FROM orders WHERE status = 'Shipped'"
	if got != want {
		t.Errorf("buildSelectSQL() = %q, want %q", got, want)
	}
}

func TestBuildInsertSQL(t *testing.T) {
	got := buildInsertSQL("postgres", "customers", []string{"id", "name", "email"})
	want := "INSERT INTO customers (id, name, email) VALUES ($1, $2, $3)"
	if got != want {
		t.Errorf("buildInsertSQL() = %q, want %q", got, want)
	}
}

func TestBuildInsertSQL_MSSQLPlaceholders(t *testing.T) {
	got := buildInsertSQL("mssql", "customers", []string{"id", "name"})
	want := "INSERT INTO customers (id, name) VALUES (@p1, @p2)"
	if got != want {
		t.Errorf("buildInsertSQL() = %q, want %q", got, want)
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `go test ./internal/clone/... -run 'TestPlaceholder|TestBuildSelectSQL|TestBuildInsertSQL' -v`
Expected: FAIL — `placeholder`/`buildSelectSQL`/`buildInsertSQL` undefined.

- [ ] **Step 7: Write `sql.go`**

```go
package clone

import (
	"fmt"
	"strings"
)

// placeholder returns the i'th (1-indexed) bound-parameter placeholder for
// engineName's driver. Clone requires source and dest to share an engine
// (see Run in run.go), so exactly one dialect's placeholder style is ever
// needed per invocation — there is no cross-dialect translation here, only
// a lookup by name.
func placeholder(engineName string, i int) string {
	switch engineName {
	case "mssql":
		return fmt.Sprintf("@p%d", i)
	case "postgres":
		return fmt.Sprintf("$%d", i)
	default: // sqlite, mysql
		return "?"
	}
}

// buildSelectSQL builds the source-side read query for one table.
// limit <= 0 means no row limit; where == "" means no filter. MSSQL has no
// LIMIT clause — it uses TOP N right after SELECT instead.
func buildSelectSQL(engineName, table string, limit int, where string) string {
	whereClause := ""
	if where != "" {
		whereClause = " WHERE " + where
	}
	if engineName == "mssql" {
		topClause := ""
		if limit > 0 {
			topClause = fmt.Sprintf("TOP %d ", limit)
		}
		return fmt.Sprintf("SELECT %s* FROM %s%s", topClause, table, whereClause)
	}
	limitClause := ""
	if limit > 0 {
		limitClause = fmt.Sprintf(" LIMIT %d", limit)
	}
	return fmt.Sprintf("SELECT * FROM %s%s%s", table, whereClause, limitClause)
}

// buildInsertSQL builds the dest-side write query for one table, with one
// bound placeholder per column in the same order columns is given.
func buildInsertSQL(engineName, table string, columns []string) string {
	placeholders := make([]string, len(columns))
	for i := range columns {
		placeholders[i] = placeholder(engineName, i+1)
	}
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, strings.Join(columns, ", "), strings.Join(placeholders, ", "))
}
```

- [ ] **Step 8: Run test to verify it passes**

Run: `go test ./internal/clone/... -v`
Expected: every test in this task PASSes.

- [ ] **Step 9: Commit**

```bash
git add internal/clone/mask.go internal/clone/mask_test.go internal/clone/sql.go internal/clone/sql_test.go
git commit -m "feat(clone): masking strategies and dialect-aware SQL building"
```

---

### Task 3: `clone.Run` — the DB-touching orchestrator

**Files:**
- Create: `internal/clone/run.go`
- Create: `internal/clone/run_test.go`

**Interfaces:**
- Consumes: `maskPlanFor`, `applyMask`, `buildSelectSQL`, `buildInsertSQL` (Task 2, same package); `config.Config`, `cfg.ResolveURL`, `cfg.EngineName` from `internal/config`; `engine.ForTarget` from `internal/engine`; `generate.TableSchema` from `internal/generate` (the return type of `Engine.Introspect`).
- Produces:
  - `type Options struct { Mask bool; Limit int; Where string }`
  - `type TableResult struct { Table string; RowsCopied int; MaskedColumns []string }`
  - `type Result struct { Source string; Dest string; Tables []TableResult }` (all three JSON-tagged for `--json` output)
  - `func Run(cfg *config.Config, sourceTarget, destTarget string, opts Options) (*Result, error)` — used by Task 4's `cmd/clone.go`.

This is tested against real SQLite files (no build tag, no Docker, no service container needed — SQLite is file-based, matching how `cmd/sqlite_loop_test.go` and `internal/down/down_test.go` already test full workflows as ordinary `go test` runs).

- [ ] **Step 1: Write the failing test**

```go
package clone

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/seanpham99/dbtools/internal/config"
	_ "github.com/seanpham99/dbtools/internal/engine/sqliteengine"
)

func setupCloneTargets(t *testing.T) (*config.Config, string, string) {
	t.Helper()
	dir := t.TempDir()
	sourceURL := "sqlite://" + filepath.Join(dir, "source.db")
	destURL := "sqlite://" + filepath.Join(dir, "dest.db")
	t.Setenv("DBTOOLS_CLONE_TEST_SOURCE_URL", sourceURL)
	t.Setenv("DBTOOLS_CLONE_TEST_DEST_URL", destURL)

	schema := `CREATE TABLE customers (id INTEGER PRIMARY KEY, name TEXT NOT NULL, email TEXT NOT NULL);`
	for _, url := range []string{sourceURL, destURL} {
		path := PathFromSQLiteURL(url)
		db, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		db.Close()
		conn, err := sqliteOpenForTest(url)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Exec(schema); err != nil {
			t.Fatal(err)
		}
		conn.Close()
	}

	cfg := &config.Config{
		Targets: map[string]config.Target{
			"prod": {URLEnv: "DBTOOLS_CLONE_TEST_SOURCE_URL"},
			"dev":  {URLEnv: "DBTOOLS_CLONE_TEST_DEST_URL"},
		},
	}
	return cfg, "prod", "dev"
}

func TestRun_CopiesRowsAndMasksEmailByDefault(t *testing.T) {
	cfg, source, dest := setupCloneTargets(t)

	sourceConn, err := sqliteOpenForTest(mustResolveURL(t, cfg, source))
	if err != nil {
		t.Fatal(err)
	}
	defer sourceConn.Close()
	if _, err := sourceConn.Exec(`INSERT INTO customers (id, name, email) VALUES (1, 'Ada', 'ada@example.com'), (2, 'Bo', 'bo@example.com')`); err != nil {
		t.Fatal(err)
	}

	result, err := Run(cfg, source, dest, Options{Mask: true})
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}
	if len(result.Tables) != 1 || result.Tables[0].Table != "customers" || result.Tables[0].RowsCopied != 2 {
		t.Fatalf("Run() result = %+v, want one customers table with 2 rows copied", result.Tables)
	}
	if len(result.Tables[0].MaskedColumns) != 1 || result.Tables[0].MaskedColumns[0] != "email" {
		t.Fatalf("Run() masked columns = %v, want [email]", result.Tables[0].MaskedColumns)
	}

	destConn, err := sqliteOpenForTest(mustResolveURL(t, cfg, dest))
	if err != nil {
		t.Fatal(err)
	}
	defer destConn.Close()
	var name, email string
	if err := destConn.QueryRow(`SELECT name, email FROM customers WHERE id = 1`).Scan(&name, &email); err != nil {
		t.Fatal(err)
	}
	if name != "Ada" {
		t.Errorf("name = %q, want Ada (non-sensitive columns copy verbatim)", name)
	}
	if email == "ada@example.com" {
		t.Error("email was not masked")
	}
}

func TestRun_NoMaskCopiesRealValues(t *testing.T) {
	cfg, source, dest := setupCloneTargets(t)
	sourceConn, err := sqliteOpenForTest(mustResolveURL(t, cfg, source))
	if err != nil {
		t.Fatal(err)
	}
	defer sourceConn.Close()
	if _, err := sourceConn.Exec(`INSERT INTO customers (id, name, email) VALUES (1, 'Ada', 'ada@example.com')`); err != nil {
		t.Fatal(err)
	}

	if _, err := Run(cfg, source, dest, Options{Mask: false}); err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	destConn, err := sqliteOpenForTest(mustResolveURL(t, cfg, dest))
	if err != nil {
		t.Fatal(err)
	}
	defer destConn.Close()
	var email string
	if err := destConn.QueryRow(`SELECT email FROM customers WHERE id = 1`).Scan(&email); err != nil {
		t.Fatal(err)
	}
	if email != "ada@example.com" {
		t.Errorf("email = %q, want unmasked ada@example.com with --no-mask", email)
	}
}

func TestRun_ClearsDestBeforeCopying(t *testing.T) {
	cfg, source, dest := setupCloneTargets(t)
	destConn, err := sqliteOpenForTest(mustResolveURL(t, cfg, dest))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := destConn.Exec(`INSERT INTO customers (id, name, email) VALUES (99, 'Stale', 'stale@example.com')`); err != nil {
		t.Fatal(err)
	}
	destConn.Close()

	if _, err := Run(cfg, source, dest, Options{Mask: false}); err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}

	destConn, err = sqliteOpenForTest(mustResolveURL(t, cfg, dest))
	if err != nil {
		t.Fatal(err)
	}
	defer destConn.Close()
	var count int
	if err := destConn.QueryRow(`SELECT COUNT(*) FROM customers WHERE id = 99`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Error("stale dest row survived clone; Run() must clear each table before copying")
	}
}

func TestRun_RejectsSameSourceAndDest(t *testing.T) {
	cfg, source, _ := setupCloneTargets(t)
	if _, err := Run(cfg, source, source, Options{Mask: true}); err == nil {
		t.Fatal("Run() with identical source and dest should error")
	}
}

func TestRun_RejectsEngineMismatch(t *testing.T) {
	cfg, source, dest := setupCloneTargets(t)
	destTarget := cfg.Targets[dest]
	destTarget.Engine = "postgres"
	cfg.Targets[dest] = destTarget
	if _, err := Run(cfg, source, dest, Options{Mask: true}); err == nil {
		t.Fatal("Run() with mismatched engines should error")
	}
}
```

This test file needs two small test-only helpers — add them at the bottom of `run_test.go`:

```go
func mustResolveURL(t *testing.T, cfg *config.Config, target string) string {
	t.Helper()
	url, err := cfg.ResolveURL(target)
	if err != nil {
		t.Fatal(err)
	}
	return url
}
```

`PathFromSQLiteURL` and `sqliteOpenForTest` are thin test-only wrappers so `run_test.go` doesn't need to import `internal/engine/sqliteengine` for anything beyond its blank-import side effect (registering the `"sqlite"` driver and engine):

```go
func PathFromSQLiteURL(rawURL string) string {
	return strings.TrimPrefix(rawURL, "sqlite://")
}

func sqliteOpenForTest(rawURL string) (*sql.DB, error) {
	return sql.Open("sqlite", PathFromSQLiteURL(rawURL))
}
```

Add `"database/sql"` and `"strings"` to the test file's imports alongside `"os"`, `"path/filepath"`, and `"testing"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/clone/... -run TestRun_ -v`
Expected: FAIL to compile — `Run`, `Options`, `Result`, `TableResult` undefined.

- [ ] **Step 3: Write `run.go`**

```go
package clone

import (
	"database/sql"
	"fmt"
	"sort"

	"github.com/seanpham99/dbtools/internal/config"
	"github.com/seanpham99/dbtools/internal/engine"
	"github.com/seanpham99/dbtools/internal/generate"
)

// Options configures one Run call.
type Options struct {
	// Mask enables the default-deny sensitive-column masking (see
	// mask.go). false is the explicit --no-mask opt-out.
	Mask bool
	// Limit caps the rows copied per table. 0 means no limit.
	Limit int
	// Where, when non-empty, is appended as-is to every table's SELECT
	// (WHERE <Where>) — an escape hatch for subsetting, trusted the same
	// way migration SQL is trusted elsewhere in dbtools.
	Where string
}

// TableResult is one table's clone outcome.
type TableResult struct {
	Table         string   `json:"table"`
	RowsCopied    int      `json:"rows_copied"`
	MaskedColumns []string `json:"masked_columns,omitempty"`
}

// Result is the full outcome of one Run call.
type Result struct {
	Source string        `json:"source"`
	Dest   string        `json:"dest"`
	Tables []TableResult `json:"tables"`
}

// Run copies every table sourceTarget's engine reports via Introspect into
// destTarget, clearing each dest table first. sourceTarget and destTarget
// must resolve to the same engine (see the design note in this plan's
// Global Constraints — cross-dialect clone is out of scope) and must be
// different targets.
func Run(cfg *config.Config, sourceTarget, destTarget string, opts Options) (*Result, error) {
	if sourceTarget == destTarget {
		return nil, fmt.Errorf("clone source and dest must be different targets (both are %q)", sourceTarget)
	}

	sourceURL, err := cfg.ResolveURL(sourceTarget)
	if err != nil {
		return nil, fmt.Errorf("source target %q: %w", sourceTarget, err)
	}
	destURL, err := cfg.ResolveURL(destTarget)
	if err != nil {
		return nil, fmt.Errorf("dest target %q: %w", destTarget, err)
	}

	sourceEng, err := engine.ForTarget(cfg.EngineName(sourceTarget), sourceURL)
	if err != nil {
		return nil, fmt.Errorf("source target %q: %w", sourceTarget, err)
	}
	destEng, err := engine.ForTarget(cfg.EngineName(destTarget), destURL)
	if err != nil {
		return nil, fmt.Errorf("dest target %q: %w", destTarget, err)
	}
	if sourceEng.Name() != destEng.Name() {
		return nil, fmt.Errorf("clone requires source and dest to use the same engine (source %q is %q, dest %q is %q)", sourceTarget, sourceEng.Name(), destTarget, destEng.Name())
	}

	sourceDB, err := sourceEng.Open(sourceURL)
	if err != nil {
		return nil, fmt.Errorf("opening source %q: %w", sourceTarget, err)
	}
	defer sourceDB.Close()

	destDB, err := destEng.Open(destURL)
	if err != nil {
		return nil, fmt.Errorf("opening dest %q: %w", destTarget, err)
	}
	defer destDB.Close()

	exclude := append(append([]string{}, cfg.Generate.Exclude...), cfg.Clone.Exclude...)
	tables, _, err := sourceEng.Introspect(sourceDB, exclude)
	if err != nil {
		return nil, fmt.Errorf("introspecting source %q: %w", sourceTarget, err)
	}

	result := &Result{Source: sourceTarget, Dest: destTarget}
	for _, tbl := range tables {
		tr, err := copyTable(sourceDB, destDB, sourceEng.Name(), tbl, cfg.Clone.Mask, opts)
		if err != nil {
			return nil, fmt.Errorf("cloning table %s: %w", tbl.Name, err)
		}
		result.Tables = append(result.Tables, *tr)
	}
	return result, nil
}

// copyTable clears tbl in destDB, then copies every row (matching opts'
// Limit/Where) from sourceDB, masking columns per plan when opts.Mask.
func copyTable(sourceDB, destDB *sql.DB, engineName string, tbl generate.TableSchema, maskConfig map[string]string, opts Options) (*TableResult, error) {
	colNames := make([]string, len(tbl.Columns))
	for i, c := range tbl.Columns {
		colNames[i] = c.Name
	}

	plan := map[string]MaskStrategy{}
	if opts.Mask {
		plan = maskPlanFor(colNames, maskConfig)
	}

	tr := &TableResult{Table: tbl.Name}
	for name := range plan {
		tr.MaskedColumns = append(tr.MaskedColumns, name)
	}
	sort.Strings(tr.MaskedColumns)

	selectSQL := buildSelectSQL(engineName, tbl.Name, opts.Limit, opts.Where)
	rows, err := sourceDB.Query(selectSQL)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", tbl.Name, err)
	}
	defer rows.Close()

	if _, err := destDB.Exec(fmt.Sprintf("DELETE FROM %s", tbl.Name)); err != nil {
		return nil, fmt.Errorf("clearing dest %s: %w", tbl.Name, err)
	}

	insertSQL := buildInsertSQL(engineName, tbl.Name, colNames)
	counters := map[string]int{}

	for rows.Next() {
		values := make([]any, len(colNames))
		ptrs := make([]any, len(colNames))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("scanning row from %s: %w", tbl.Name, err)
		}
		for i, colName := range colNames {
			if strat, ok := plan[colName]; ok {
				values[i] = applyMask(strat, values[i], counters, colName)
			}
		}
		if _, err := destDB.Exec(insertSQL, values...); err != nil {
			return nil, fmt.Errorf("inserting row into %s: %w", tbl.Name, err)
		}
		tr.RowsCopied++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating rows from %s: %w", tbl.Name, err)
	}
	return tr, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/clone/... -v`
Expected: every test in Tasks 2 and 3 PASSes.

- [ ] **Step 5: Commit**

```bash
git add internal/clone/run.go internal/clone/run_test.go
git commit -m "feat(clone): Run orchestrator — introspect, mask, copy, dest-clear"
```

---

### Task 4: `dbtools clone` command

**Files:**
- Create: `cmd/clone.go`
- Create: `cmd/clone_test.go`

**Interfaces:**
- Consumes: `clone.Run`, `clone.Options`, `clone.Result` (Task 3); `loadConfig` (package-level var already defined in `cmd/reset.go`, reused — do not redeclare it); `requireUnprotected` (already defined in `cmd/openTarget.go`).
- Produces: `cloneCmd *cobra.Command`, `runClone(source, dest string) error`, a package-level `cloneRun = clone.Run` var (for test stubbing, matching the existing `applyRun = apply.Run` / `seedRun = seed.Run` pattern in `cmd/reset.go`).

- [ ] **Step 1: Write the failing tests**

```go
package cmd

import (
	"testing"

	"github.com/seanpham99/dbtools/internal/clone"
	"github.com/seanpham99/dbtools/internal/config"
)

func TestCloneCmd_RequiresYesFlag(t *testing.T) {
	cloneYes = false
	t.Cleanup(func() { cloneYes = false })
	err := cloneCmd.RunE(cloneCmd, []string{"prod", "dev"})
	if err == nil {
		t.Fatal("expected error when --yes is not set, got nil")
	}
}

func TestCloneCmd_RejectsBothMaskFlags(t *testing.T) {
	cloneYes = true
	cloneMask = true
	cloneNoMask = true
	t.Cleanup(func() { cloneYes, cloneMask, cloneNoMask = false, false, false })
	err := cloneCmd.RunE(cloneCmd, []string{"prod", "dev"})
	if err == nil {
		t.Fatal("expected error when both --mask and --no-mask are set, got nil")
	}
}

func TestRunClone_RefusesProtectedDest(t *testing.T) {
	origLoadConfig := loadConfig
	t.Cleanup(func() { loadConfig = origLoadConfig })
	loadConfig = func(string) (*config.Config, error) {
		return &config.Config{
			Targets: map[string]config.Target{
				"prod": {URLEnv: "DBTOOLS_CLONE_PROD_URL"},
				"dev":  {URLEnv: "DBTOOLS_CLONE_DEV_URL", Protected: true},
			},
		}, nil
	}
	cloneYes = true
	t.Cleanup(func() { cloneYes = false })

	if err := runClone("prod", "dev"); err == nil {
		t.Fatal("runClone() into a protected dest should refuse")
	}
}

func TestRunClone_CallsCloneRunWithResolvedOptions(t *testing.T) {
	origLoadConfig := loadConfig
	origCloneRun := cloneRun
	t.Cleanup(func() {
		loadConfig = origLoadConfig
		cloneRun = origCloneRun
	})
	loadConfig = func(string) (*config.Config, error) {
		return &config.Config{
			Targets: map[string]config.Target{
				"prod": {URLEnv: "DBTOOLS_CLONE_PROD_URL"},
				"dev":  {URLEnv: "DBTOOLS_CLONE_DEV_URL"},
			},
		}, nil
	}
	var gotSource, gotDest string
	var gotOpts clone.Options
	cloneRun = func(cfg *config.Config, source, dest string, opts clone.Options) (*clone.Result, error) {
		gotSource, gotDest, gotOpts = source, dest, opts
		return &clone.Result{Source: source, Dest: dest}, nil
	}

	cloneYes = true
	cloneNoMask = true
	cloneLimit = 50
	cloneWhere = "status = 'Shipped'"
	t.Cleanup(func() { cloneYes, cloneNoMask, cloneLimit, cloneWhere = false, false, 0, "" })

	if err := runClone("prod", "dev"); err != nil {
		t.Fatalf("runClone() returned error: %v", err)
	}
	if gotSource != "prod" || gotDest != "dev" {
		t.Errorf("cloneRun called with (%q, %q), want (prod, dev)", gotSource, gotDest)
	}
	if gotOpts.Mask {
		t.Errorf("Options.Mask = true, want false (--no-mask was set)")
	}
	if gotOpts.Limit != 50 || gotOpts.Where != "status = 'Shipped'" {
		t.Errorf("Options = %+v, want Limit=50 Where=\"status = 'Shipped'\"", gotOpts)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/... -run TestCloneCmd -v`
Expected: FAIL to compile — `cloneCmd`, `cloneYes`, `cloneMask`, `cloneNoMask`, `runClone`, `cloneRun` undefined.

- [ ] **Step 3: Write `cmd/clone.go`**

```go
package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/seanpham99/dbtools/internal/clone"
	"github.com/spf13/cobra"
)

var cloneRun = clone.Run

var (
	cloneYes    bool
	cloneMask   bool
	cloneNoMask bool
	cloneLimit  int
	cloneWhere  string
)

var cloneCmd = &cobra.Command{
	Use:   "clone <source> <dest>",
	Short: "Copy data from source target into dest target, masking sensitive columns by default",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if cloneMask && cloneNoMask {
			return fmt.Errorf("--mask and --no-mask are mutually exclusive")
		}
		if !cloneYes {
			return fmt.Errorf("clone overwrites every cloned table in %q — pass --yes to confirm", args[1])
		}
		return runClone(args[0], args[1])
	},
}

func init() {
	cloneCmd.Flags().BoolVar(&cloneYes, "yes", false, "confirm overwriting dest's data")
	cloneCmd.Flags().BoolVar(&cloneMask, "mask", false, "mask sensitive columns (default; --no-mask opts out)")
	cloneCmd.Flags().BoolVar(&cloneNoMask, "no-mask", false, "copy sensitive columns unmasked — a documented PII risk")
	cloneCmd.Flags().IntVar(&cloneLimit, "limit", 0, "copy at most this many rows per table (0 = no limit)")
	cloneCmd.Flags().StringVar(&cloneWhere, "where", "", "SQL filter appended to every table's SELECT (trusted, unsanitized)")
	rootCmd.AddCommand(cloneCmd)
}

func runClone(sourceTarget, destTarget string) error {
	cfg, err := loadConfig("dbtools.toml")
	if err != nil {
		return fmt.Errorf("loading dbtools.toml: %w", err)
	}
	// Clone is exactly as destructive to dest as reset is — no --yes
	// override for a protected dest, unlike push's softer rule. See this
	// plan's Global Constraints for why.
	if err := requireUnprotected(cfg, destTarget); err != nil {
		return err
	}

	result, err := cloneRun(cfg, sourceTarget, destTarget, clone.Options{
		Mask:  !cloneNoMask,
		Limit: cloneLimit,
		Where: cloneWhere,
	})
	if err != nil {
		return err
	}

	if jsonOutput {
		b, err := json.Marshal(result)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}

	fmt.Printf("%s -> %s: cloned %d table(s)\n", result.Source, result.Dest, len(result.Tables))
	for _, tr := range result.Tables {
		maskNote := ""
		if len(tr.MaskedColumns) > 0 {
			maskNote = fmt.Sprintf(" (masked: %v)", tr.MaskedColumns)
		}
		fmt.Printf("  %-20s %d row(s)%s\n", tr.Table, tr.RowsCopied, maskNote)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/... -run TestCloneCmd -v && go test ./cmd/... -run TestRunClone -v`
Expected: PASS.

- [ ] **Step 5: Run the full test suite to catch any regressions**

Run: `go build ./... && go test ./...`
Expected: all green.

- [ ] **Step 6: Commit**

```bash
git add cmd/clone.go cmd/clone_test.go
git commit -m "feat: add dbtools clone command"
```

---

### Task 5: Docs

**Files:**
- Modify: `README.md`
- Modify: `docs/roadmap.md`
- Modify: `skills/using-dbtools/SKILL.md`

**Interfaces:** none — documentation only.

- [ ] **Step 1: Add `clone` to the CLI Command Reference table in `README.md`**

Add a row after `generate`:

```markdown
| `clone` | `dbtools clone <source> <dest> [--yes] [--no-mask] [--limit N] [--where "SQL"]` | Copies data from `source` into `dest` (same engine only), masking sensitive columns by default. `dest` must not be protected. |
```

Add a short new subsection after "## Python Pydantic Model Generation" / before "## Agent & CI Ergonomics":

```markdown
## Clone (prod → dev)

Refresh a local or dev database from a snapshot of another target's data,
without hand-rolling a dump/restore/scrub script:

\`\`\`bash
dbtools clone prod dev --yes
\`\`\`

Masking is on by default: any column literally named `email`, `phone`,
`ssn`, or `password` is masked automatically (email columns get a
deterministic synthetic address; the rest are redacted). Add explicit
overrides in `dbtools.toml`:

\`\`\`toml
[clone]
exclude = ["audit_log"]

[clone.mask]
customerName = "hash"
\`\`\`

Raw, unmasked copies are an explicit opt-out (`--no-mask`) — document why
before using it; it is a PII risk. `source` and `dest` must use the same
engine, and `dest` must not be `protected` (clone always clears and
repopulates every cloned table).
```

(Use literal triple backticks in the actual file — escaped above only for this plan's own Markdown nesting.)

- [ ] **Step 2: Update `docs/roadmap.md`**

The phases table row:

```markdown
| v0.3/4 | **Clone prod→dev** | Schema + data clone with config-driven masking. Masking on by default; raw copy requires explicit opt-out. |
```

becomes:

```markdown
| v0.3/4 ✅ | **Clone prod→dev** | Shipped: `dbtools clone <source> <dest>`, `internal/clone`. Masking on by default (built-in sensitive-column list + `[clone.mask]` overrides); `--no-mask` is the explicit opt-out. Same-engine only; row-count/WHERE subsetting via `--limit`/`--where`. |
```

Also update the "## Clone (prod→dev)" design section further down the same file — its bullet list currently describes the feature prospectively (`dbtools clone <source> <dest> [--mask|--no-mask]`); replace the whole section with:

```markdown
## Clone (prod→dev)

Shipped. See `internal/clone` and the "Clone (prod → dev)" section of
README.md for usage. Implementation plan (for reference):
`docs/superpowers/plans/2026-08-25-clone-prod-to-dev.md`.
```

- [ ] **Step 3: Add `clone` to the agent-facing skill's Quick Command Reference in `skills/using-dbtools/SKILL.md`**

Add after the `generate --lang ts` line:

```markdown
# Copy data from one target into another (same engine only), masking
# sensitive columns by default — refresh dev from a prod snapshot
dbtools clone <source> <dest> --yes [--no-mask] [--limit N] [--where "SQL"]
```

Add a row to the terminology table:

```markdown
| **clone** | Copies data from one target into another of the same engine, masking sensitive columns by default (`--no-mask` opts out). Data-only — schema must already match (both targets share one `migrations_dir`). | `sync`, `restore` |
```

- [ ] **Step 4: Run the full local verification pass**

```bash
scripts/dev-local.sh all
```

Expected: all green.

- [ ] **Step 5: Commit**

```bash
git add README.md docs/roadmap.md skills/using-dbtools/SKILL.md
git commit -m "docs: document dbtools clone"
```

---

## Self-review notes (for whoever executes this plan)

- **Spec coverage:** roadmap's Clone entry lists `--mask|--no-mask` (Task 4), config-driven masking (Task 1 + Task 2's `maskPlanFor`), default-deny on sensitive columns (Task 2's `builtinSensitiveColumns`), masking-on-by-default with explicit `--no-mask` opt-out (Task 4's `runClone`), and optional subsetting via row-count/WHERE (Task 3/4's `Limit`/`Where`) — all covered.
- **Scope calls made explicitly, not left as TBD:** same-engine-only (Global Constraints), dest-protected always refuses regardless of `--yes` (Global Constraints, contrasted explicitly against `push`'s softer rule), `DELETE FROM` for clearing dest rather than per-dialect `TRUNCATE` (portability), no FK-dependency-order solving — clone copies tables in `Introspect()` order and surfaces a real constraint-violation error if that order doesn't satisfy the dest schema's FKs (documented as a known v1 limitation, not silently swallowed).
- **Type/signature consistency check:** `clone.Options.Mask`/`Limit`/`Where` field names and types match between Task 3's definition and Task 4's `runClone` construction (`clone.Options{Mask: !cloneNoMask, Limit: cloneLimit, Where: cloneWhere}`); `clone.Run`'s signature `(cfg *config.Config, sourceTarget, destTarget string, opts Options) (*Result, error)` matches both the `cloneRun` package var's inferred type in `cmd/clone.go` and the stub assigned to it in `TestRunClone_CallsCloneRunWithResolvedOptions`.
- **Known v1 limitations to flag in the PR description:** no FK-dependency-ordering or cross-dialect clone; `--where`/`--limit` apply identically to every table (no per-table subsetting); large tables are copied row-by-row with no batching/streaming tuning (fine for a "refresh my dev database" use case, not benchmarked for multi-million-row prod tables — a real follow-up if that ever matters).
