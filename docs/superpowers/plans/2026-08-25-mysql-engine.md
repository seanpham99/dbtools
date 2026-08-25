# MySQL Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add MySQL as dbtools's 4th supported migration engine, so `mysql://` target URLs work identically to `mssql://`/`postgres://`/`sqlite://` across every command (`up`, `push`, `verify`, `repair`, `generate`, `doctor`, ...).

**Architecture:** Plug into the existing `internal/engine.Engine` seam exactly the way `mssqlengine`/`postgresengine`/`sqliteengine` already do — a new `internal/engine/mysqlengine` package that self-registers in `init()`, implements `Name`/`Open`/`DDL`/`Ledger`/`Introspect`, and a blank-import migrate driver registration in `internal/migrator`. No changes to any engine-agnostic package (`cmd/*`, `internal/apply`, `internal/verify`, `internal/ledger`, `internal/generate`) are needed — that's the point of the seam.

**Tech Stack:** Go 1.25, `github.com/go-sql-driver/mysql` (already an indirect dependency via golang-migrate, this plan promotes it to direct), `github.com/golang-migrate/migrate/v4/database/mysql` (self-registers under the `"mysql"` URL scheme — confirmed by reading `database/mysql/mysql.go` in the vendored module: `mysql.ParseDSN(strings.TrimPrefix(url, "mysql://"))`, so dbtools's own `mysql://` scheme needs no rewriting, unlike MSSQL's `mssql://`→`sqlserver://`).

## Global Constraints

- Go 1.25+, matches `go.mod`.
- Never hardcode a connection string or credential anywhere (code, tests, CI YAML) — tests use `t.Setenv`; CI uses service-container env vars and GitHub Secrets only where a real password is needed (this plan's MySQL CI password is a throwaway container-local value, not a secret, matching the existing Postgres job's pattern — not the MSSQL job's, which does use a secret).
- Target URL convention for this engine: `mysql://user:pass@tcp(host:port)/dbname?param=value` (golang-migrate's own documented mysql DSN shape — the `tcp(...)` wrapping is required by `go-sql-driver/mysql`'s DSN grammar, not optional).
- `dbtools.toml` never stores a literal URL — only `url_env`, unchanged by this plan.
- Follow the existing per-dialect file layout exactly (mirror `internal/engine/mssqlengine`'s 5-file split: `mysqlengine.go`, `conn.go`, `ddl.go`, `introspect.go`, `ledger.go`) since MySQL, like MSSQL, needs real DSN translation logic worth its own file.
- Scope for `DDL()`: **`TABLE` and `VIEW` only** (mirrors `sqliteengine`, not `mssqlengine`/`postgresengine`'s procedure/function support). MySQL stored procedures/functions require client-side `DELIMITER` handling to parse correctly, which is a real parsing problem distinct from everything else in `ddlcheck` and out of scope for this plan — classicmodels needs only tables/views, and the roadmap entry for MySQL doesn't call for proc support.
- Scope for `internal/container` (the `dbtools start`/`stop` local Docker lifecycle): **out of scope**. The roadmap entry only promises "add MySQL as a supported migration engine" + the classicmodels fixture corpus; container support is a separate, unpromised enhancement. Note this explicitly in the PR description when this plan ships.

---

### Task 1: Register the golang-migrate MySQL driver

**Files:**

- Create: `internal/migrator/mysqldriver.go`
- Modify: `go.mod` (via `go mod tidy` after Task 2 adds the direct import — do not hand-edit `go.mod` in this task)

**Interfaces:**

- Consumes: nothing new.
- Produces: golang-migrate's `"mysql"` URL scheme is registered process-wide once this file is imported anywhere in the binary (it is, transitively, via `internal/engine/mysqlengine` in Task 2). No exported Go symbols.

- [ ] **Step 1: Create the blank-import file**

This mirrors `internal/migrator/pgdriver.go` and `internal/migrator/sqlitedriver.go` exactly — MySQL needs no batch-splitting wrapper (that's MSSQL-specific, for GO-separated batches) and no per-migration session reset (that's Postgres-specific, for `search_path` poisoning from `pg_dump` baselines). MySQL has neither problem: migration files execute as-is.

```go
package migrator

// golang-migrate's mysql driver self-registers under the "mysql" URL scheme
// on import (confirmed in its Open(): it does
// mysql.ParseDSN(strings.TrimPrefix(url, "mysql://")) — no scheme rewrite
// needed, unlike MSSQL's mssql:// -> sqlserver:// wrapper). No batch
// splitting or session reset needed: migration files execute as-is.
import (
 _ "github.com/golang-migrate/migrate/v4/database/mysql"
)
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/migrator/...`
Expected: succeeds (the import will fail to resolve until Task 2's `go mod tidy`, so if this fails with a missing-module error, that's expected at this point — proceed to Task 2 first, then return and re-run this build).

- [ ] **Step 3: Commit**

```bash
git add internal/migrator/mysqldriver.go
git commit -m "feat(migrator): register golang-migrate mysql driver"
```

---

### Task 2: MySQL DSN translation (`dsnFromURL`) — pure, unit-testable

**Files:**

- Create: `internal/engine/mysqlengine/conn.go`
- Create: `internal/engine/mysqlengine/conn_test.go`

**Interfaces:**

- Consumes: `github.com/go-sql-driver/mysql` (`mysql.ParseDSN`, `mysql.Config.FormatDSN`).
- Produces:
  - `func dsnFromURL(rawURL string) (string, error)` — used by Task 3's `Open`.
  - `func Open(rawURL string) (*sql.DB, error)` — used by `mysqlengine.go` (Task 3).

This is the one piece of real logic worth testing without a database: converting dbtools's `mysql://` target URL into the DSN `go-sql-driver/mysql` expects, and forcing `parseTime=true` so `DATETIME`/`TIMESTAMP` columns scan into `time.Time`/`sql.NullTime` instead of `[]byte` (a footgun every other engine in this repo doesn't have, since `go-mssqldb` and `lib/pq` don't require an opt-in flag for this).

- [ ] **Step 1: Write the failing test**

```go
package mysqlengine

import "testing"

func TestDSNFromURL_StripsSchemeAndForcesParseTime(t *testing.T) {
 got, err := dsnFromURL("mysql://root:secret@tcp(127.0.0.1:3306)/dbtools_local")
 if err != nil {
  t.Fatalf("dsnFromURL() returned error: %v", err)
 }
 want := "root:secret@tcp(127.0.0.1:3306)/dbtools_local?parseTime=true"
 if got != want {
  t.Errorf("dsnFromURL() = %q, want %q", got, want)
 }
}

func TestDSNFromURL_PreservesExistingParamsAndOverridesParseTime(t *testing.T) {
 // A caller-supplied parseTime=false must not silently defeat the
 // DATETIME-scanning requirement — dbtools always forces it on.
 got, err := dsnFromURL("mysql://u:p@tcp(h:3306)/d?parseTime=false&tls=skip-verify")
 if err != nil {
  t.Fatalf("dsnFromURL() returned error: %v", err)
 }
 want := "u:p@tcp(h:3306)/d?parseTime=true&tls=skip-verify"
 if got != want {
  t.Errorf("dsnFromURL() = %q, want %q", got, want)
 }
}

func TestDSNFromURL_InvalidDSN(t *testing.T) {
 if _, err := dsnFromURL("mysql://not a valid dsn!!!"); err == nil {
  t.Fatal("dsnFromURL() with a malformed DSN returned nil error, want an error")
 }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/mysqlengine/... -run TestDSNFromURL -v`
Expected: FAIL — `dsnFromURL` is undefined (package doesn't exist yet).

- [ ] **Step 3: Write the implementation**

```go
package mysqlengine

import (
 "database/sql"
 "fmt"
 "strings"

 "github.com/go-sql-driver/mysql"
)

// dsnFromURL converts a dbtools "mysql://" target URL into the DSN
// github.com/go-sql-driver/mysql expects: golang-migrate's own mysql
// driver does the same TrimPrefix + ParseDSN (see
// database/mysql/mysql.go's urlToMySQLConfig), so dbtools's raw mysql://
// URLs already match the format golang-migrate itself requires.
//
// ParseTime is always forced true: without it, DATETIME/TIMESTAMP columns
// scan as []byte instead of time.Time/sql.NullTime, which would silently
// break the ledger's recorded_at column (internal/ledger.Entry.RecordedAt
// is *time.Time) for every caller who forgot the query param.
func dsnFromURL(rawURL string) (string, error) {
 raw := strings.TrimPrefix(rawURL, "mysql://")
 cfg, err := mysql.ParseDSN(raw)
 if err != nil {
  return "", fmt.Errorf("parsing mysql DSN: %w", err)
 }
 cfg.ParseTime = true
 return cfg.FormatDSN(), nil
}

// Open opens a direct database/sql connection to rawURL (a dbtools
// "mysql://"-scheme connection string), for callers that need raw SQL
// access alongside golang-migrate's own tracked connection.
func Open(rawURL string) (*sql.DB, error) {
 dsn, err := dsnFromURL(rawURL)
 if err != nil {
  return nil, err
 }
 db, err := sql.Open("mysql", dsn)
 if err != nil {
  return nil, fmt.Errorf("opening database connection: %w", err)
 }
 return db, nil
}
```

- [ ] **Step 4: Promote the dependency and run the test**

```bash
go mod tidy
go test ./internal/engine/mysqlengine/... -run TestDSNFromURL -v
```

Expected: `go mod tidy` moves `github.com/go-sql-driver/mysql` from an indirect (`// indirect`) to a direct requirement in `go.mod`; all three `TestDSNFromURL_*` cases PASS.

- [ ] **Step 5: Re-run Task 1's build to confirm the driver import now resolves**

Run: `go build ./...`
Expected: succeeds.

- [ ] **Step 6: Commit**

```bash
git add internal/engine/mysqlengine/conn.go internal/engine/mysqlengine/conn_test.go go.mod go.sum internal/migrator/mysqldriver.go
git commit -m "feat(mysqlengine): DSN translation and Open()"
```

---

### Task 3: The `MySQL` engine type and self-registration

**Files:**

- Create: `internal/engine/mysqlengine/mysqlengine.go`
- Create: `internal/engine/mysqlengine/mysqlengine_test.go`

**Interfaces:**

- Consumes: `dsnFromURL`/`Open` (Task 2, same package); `engine.Register`, `engine.DDLDialect`, `engine.LedgerStore` from `internal/engine`; `generate.TableSchema` from `internal/generate`.
- Produces: `type MySQL struct{}` satisfying `engine.Engine`, registered under the name `"mysql"`. `mysqlDDL{}` and `mysqlLedgerStore{}` are referenced here but defined in Tasks 4/5 — this task will not compile standalone; that's expected, proceed to Tasks 4/5 before running this task's test.

- [ ] **Step 1: Write the failing test**

```go
package mysqlengine

import (
 "testing"

 "github.com/seanpham99/dbtools/internal/engine"
)

// The MySQL engine registers itself in init(); mysql:// URLs must resolve
// to it, and its dialect hooks must be wired.
func TestMySQLRegistered(t *testing.T) {
 e, err := engine.ForURL("mysql://root:x@tcp(127.0.0.1:3306)/dbtools_test")
 if err != nil {
  t.Fatalf("ForURL(mysql://...) returned error: %v", err)
 }
 if e.Name() != "mysql" {
  t.Fatalf("resolved engine %q, want mysql", e.Name())
 }
 if e.DDL() == nil || e.Ledger() == nil {
  t.Fatal("MySQL engine must provide DDL and Ledger dialects")
 }

 objs := e.DDL().ExtractObjects("CREATE TABLE `users` (id INT);")
 if len(objs) != 1 || objs[0].Name != "users" {
  t.Fatalf("DDL().ExtractObjects() = %+v, want one users ref", objs)
 }
}

func TestForTargetValidatesConfiguredEngine(t *testing.T) {
 if _, err := engine.ForTarget("mysql", "mysql://u:p@tcp(h:3306)/db"); err != nil {
  t.Fatalf("ForTarget(mysql, mysql://) returned error: %v", err)
 }
 if _, err := engine.ForTarget("mysql", "postgres://u:p@h/db"); err == nil {
  t.Fatal("ForTarget(mysql, postgres://) should fail")
 }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/mysqlengine/... -run TestMySQLRegistered -v`
Expected: FAIL to compile — `mysqlDDL`/`mysqlLedgerStore`/`MySQL` undefined.

- [ ] **Step 3: Write the implementation**

```go
// Package mysqlengine is the MySQL implementation of the engine seam. Its
// golang-migrate driver self-registers under the "mysql" scheme in
// internal/migrator/mysqldriver.go.
package mysqlengine

import (
 "database/sql"

 "github.com/seanpham99/dbtools/internal/engine"
 "github.com/seanpham99/dbtools/internal/generate"
)

func init() {
 engine.Register(MySQL{})
}

// MySQL is the MySQL engine.
type MySQL struct{}

func (MySQL) Name() string { return "mysql" }

func (MySQL) Open(rawURL string) (*sql.DB, error) { return Open(rawURL) }

func (MySQL) DDL() engine.DDLDialect { return mysqlDDL{} }

func (MySQL) Ledger() engine.LedgerStore { return mysqlLedgerStore{} }

func (MySQL) Introspect(db *sql.DB, excludeList []string) ([]generate.TableSchema, []string, error) {
 return introspect(db, excludeList)
}
```

- [ ] **Step 4: Implement Tasks 4 and 5 (below), then run this test**

Run: `go test ./internal/engine/mysqlengine/... -run 'TestMySQLRegistered|TestForTargetValidatesConfiguredEngine' -v`
Expected: PASS (after `mysqlDDL` and `mysqlLedgerStore` exist).

- [ ] **Step 5: Commit** (fold into Task 5's commit, since this file doesn't compile alone — see Task 5's final commit step, which includes this file)

---

### Task 4: DDL dialect (`ExtractObjects`, `ExtractDroppedObjects`, `Exists`)

**Files:**

- Create: `internal/engine/mysqlengine/ddl.go`
- Create: `internal/engine/mysqlengine/ddl_test.go`

**Interfaces:**

- Consumes: `ddlcheck.ObjectRef` from `internal/ddlcheck`.
- Produces: `type mysqlDDL struct{}` implementing `engine.DDLDialect` (used by Task 3's `MySQL.DDL()`).

**Design note on `Schema`:** unlike MSSQL (`dbo`), Postgres (`public`), and SQLite (`main`), MySQL has no schema layer separate from the database itself — "schema" and "database" are the same thing, and it's whatever database the connection is using (`DATABASE()`), which varies per target. There is no fixed literal to default to. This dialect leaves `ObjectRef.Schema` as `""` for an unqualified name (the overwhelming common case — migrations don't cross-reference other databases) and only fills it in when a migration explicitly qualifies a name as `` `db`.`table` ``. `Exists()` interprets an empty `Schema` as "use `DATABASE()`". This is a real behavior difference from the other three dialects, not a bug — `internal/verify`'s `Detail` messages will print a bare `.tablename` for the common unqualified case, which is cosmetically different from `dbo.tablename` but not functionally broken (confirmed: `ddlcheck.ObjectRef` is a plain struct, `verify.Collect` and `internal/ledger` never require `Schema` to be non-empty — it's only used as a map key and in a format string).

- [ ] **Step 1: Write the failing test**

```go
package mysqlengine

import (
 "testing"

 "github.com/seanpham99/dbtools/internal/ddlcheck"
)

func TestExtractObjects_CreateTableBacktickQuoted(t *testing.T) {
 sql := "CREATE TABLE `widget_order` (\n    `widget_order_id` BIGINT AUTO_INCREMENT PRIMARY KEY\n);"
 got := mysqlDDL{}.ExtractObjects(sql)
 want := ddlcheck.ObjectRef{Schema: "", Name: "widget_order", Kind: "table"}
 if len(got) != 1 || got[0] != want {
  t.Fatalf("ExtractObjects() = %+v, want [%+v]", got, want)
 }
}

func TestExtractObjects_CreateTableIfNotExistsUnquoted(t *testing.T) {
 sql := "CREATE TABLE IF NOT EXISTS orders (id INT);"
 got := mysqlDDL{}.ExtractObjects(sql)
 want := ddlcheck.ObjectRef{Schema: "", Name: "orders", Kind: "table"}
 if len(got) != 1 || got[0] != want {
  t.Fatalf("ExtractObjects() = %+v, want [%+v]", got, want)
 }
}

func TestExtractObjects_View(t *testing.T) {
 sql := "CREATE OR REPLACE VIEW `active_users` AS SELECT * FROM users;"
 got := mysqlDDL{}.ExtractObjects(sql)
 want := ddlcheck.ObjectRef{Schema: "", Name: "active_users", Kind: "view"}
 if len(got) != 1 || got[0] != want {
  t.Fatalf("ExtractObjects() = %+v, want [%+v]", got, want)
 }
}

func TestExtractObjects_AlterAndIndexNotExtracted(t *testing.T) {
 sql := "ALTER TABLE widget_order ADD amount DECIMAL(19,6);\nCREATE INDEX ix_amount ON widget_order(amount);"
 got := mysqlDDL{}.ExtractObjects(sql)
 if len(got) != 0 {
  t.Errorf("ExtractObjects() = %+v, want 0 objects (ALTER/INDEX out of scope)", got)
 }
}

func TestExtractDroppedObjects_GuardedDrop(t *testing.T) {
 sql := "DROP TABLE IF EXISTS `legacy_widget_tracking`;"
 got := mysqlDDL{}.ExtractDroppedObjects(sql)
 want := ddlcheck.ObjectRef{Schema: "", Name: "legacy_widget_tracking", Kind: "table"}
 if len(got) != 1 || got[0] != want {
  t.Fatalf("ExtractDroppedObjects() = %+v, want [%+v]", got, want)
 }
}

func TestExtractDroppedObjects_View(t *testing.T) {
 sql := "DROP VIEW active_users;"
 got := mysqlDDL{}.ExtractDroppedObjects(sql)
 want := ddlcheck.ObjectRef{Schema: "", Name: "active_users", Kind: "view"}
 if len(got) != 1 || got[0] != want {
  t.Fatalf("ExtractDroppedObjects() = %+v, want [%+v]", got, want)
 }
}

func TestExistsRejectsUnknownKind(t *testing.T) {
 if _, err := (mysqlDDL{}).Exists(nil, ddlcheck.ObjectRef{Kind: "procedure"}); err == nil {
  t.Fatal("Exists() with an out-of-scope kind should fail")
 }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/mysqlengine/... -run 'TestExtractObjects|TestExtractDroppedObjects|TestExistsRejectsUnknownKind' -v`
Expected: FAIL to compile — `mysqlDDL` undefined.

- [ ] **Step 3: Write the implementation**

```go
package mysqlengine

import (
 "database/sql"
 "fmt"
 "regexp"
 "strings"

 "github.com/seanpham99/dbtools/internal/ddlcheck"
)

// identifier matches an optionally backtick-quoted MySQL identifier.
const identifier = "`?([A-Za-z_][A-Za-z0-9_]*)`?"

// createObjectPattern matches a top-level CREATE [OR REPLACE] [TEMPORARY]
// TABLE|VIEW [IF NOT EXISTS] [db.]name statement. See the package's DDL
// scope note (docs/superpowers/plans/2026-08-25-mysql-engine.md, Task 4):
// procedures/functions are out of scope.
var createObjectPattern = regexp.MustCompile(
 `(?im)^\s*CREATE\s+(?:OR\s+REPLACE\s+)?(?:TEMPORARY\s+)?(TABLE|VIEW)\s+(?:IF\s+NOT\s+EXISTS\s+)?` +
  `(?:` + identifier + `\.)?` + identifier)

var dropObjectPattern = regexp.MustCompile(
 `(?im)^\s*DROP\s+(TABLE|VIEW)\s+(?:IF\s+EXISTS\s+)?` +
  `(?:` + identifier + `\.)?` + identifier)

func refsFrom(matches [][]string) []ddlcheck.ObjectRef {
 objects := make([]ddlcheck.ObjectRef, 0, len(matches))
 for _, m := range matches {
  objects = append(objects, ddlcheck.ObjectRef{
   // Schema stays "" for the common unqualified case — see the
   // package-level design note on why MySQL has no fixed default
   // schema literal the way dbo/public/main are for the other
   // three dialects.
   Schema: m[2],
   Name:   m[3],
   Kind:   strings.ToLower(m[1]),
  })
 }
 return objects
}

type mysqlDDL struct{}

func (mysqlDDL) ExtractObjects(sqlText string) []ddlcheck.ObjectRef {
 return refsFrom(createObjectPattern.FindAllStringSubmatch(sqlText, -1))
}

func (mysqlDDL) ExtractDroppedObjects(sqlText string) []ddlcheck.ObjectRef {
 return refsFrom(dropObjectPattern.FindAllStringSubmatch(sqlText, -1))
}

// Exists reports whether ref currently exists in db. An empty ref.Schema
// means "the database this connection is currently using" (DATABASE()).
func (mysqlDDL) Exists(db *sql.DB, ref ddlcheck.ObjectRef) (bool, error) {
 var typeFilter string
 switch ref.Kind {
 case "table":
  typeFilter = "TABLE_TYPE = 'BASE TABLE'"
 case "view":
  typeFilter = "TABLE_TYPE = 'VIEW'"
 default:
  return false, fmt.Errorf("unknown object kind %q", ref.Kind)
 }

 schemaClause := "TABLE_SCHEMA = DATABASE()"
 args := []any{ref.Name}
 if ref.Schema != "" {
  schemaClause = "TABLE_SCHEMA = ?"
  args = []any{ref.Schema, ref.Name}
 }

 query := fmt.Sprintf(`
SELECT COUNT(*)
FROM information_schema.tables
WHERE %s AND TABLE_NAME = ? AND (%s)`, schemaClause, typeFilter)

 var count int
 if err := db.QueryRow(query, args...).Scan(&count); err != nil {
  return false, fmt.Errorf("checking existence of %s.%s: %w", ref.Schema, ref.Name, err)
 }
 return count > 0, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/mysqlengine/... -run 'TestExtractObjects|TestExtractDroppedObjects|TestExistsRejectsUnknownKind' -v`
Expected: PASS (the DB-touching path in `Exists` isn't exercised by `TestExistsRejectsUnknownKind` since it returns before querying — `db` can stay `nil`).

- [ ] **Step 5: Commit** (fold into Task 5's commit — `mysqlengine.go` from Task 3 still won't compile alone until `mysqlLedgerStore` exists)

---

### Task 5: Ledger dialect (`mysqlLedgerStore`)

**Files:**

- Create: `internal/engine/mysqlengine/ledger.go`
- Create: `internal/engine/mysqlengine/ledger_integration_test.go` (build-tagged `integration`, mirrors `internal/engine/mssqlengine/ledger_integration_test.go` — every ledger operation needs a real database, there is no pure-logic slice to unit test here, matching this repo's existing convention for the ledger dialects)

**Interfaces:**

- Consumes: `ledger.DBTX`, `ledger.Entry`, `ledger.Status`, `ledger.StatusApplied`, `ledger.StatusReverted` from `internal/ledger`; `migrator.Migrator`, `migrator.ListVersions` from `internal/migrator`.
- Produces: `type mysqlLedgerStore struct{}` implementing `engine.LedgerStore` (used by Task 3's `MySQL.Ledger()`). Completes `mysqlengine.go` — after this task, the whole package compiles and Task 3's test can pass.

- [ ] **Step 1: Write the implementation directly**

Ledger correctness here can only be verified against a live server (every method executes SQL), so this task writes the implementation and its integration test together rather than red/green on a compile-only stub — matching how `internal/engine/postgresengine/ledger.go` and `internal/engine/sqliteengine/ledger.go` were built in this repo (no non-integration test file exists for either).

```go
package mysqlengine

import (
 "database/sql"
 "fmt"
 "math"

 "github.com/seanpham99/dbtools/internal/ledger"
 "github.com/seanpham99/dbtools/internal/migrator"
)

// mysqlLedgerStore is the MySQL dialect of the dbtools_migration_history
// ledger. Semantics match internal/ledger exactly — only the SQL differs.
type mysqlLedgerStore struct{}

func (mysqlLedgerStore) ensureSchema(db ledger.DBTX) error {
 _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS dbtools_migration_history (
    version         BIGINT       NOT NULL PRIMARY KEY,
    status          VARCHAR(10)  NOT NULL,
    recorded_at     DATETIME     NULL,
    note            VARCHAR(400) NULL,
    content_sha256  CHAR(64)     NULL,
    CHECK (status IN ('applied', 'reverted'))
) ENGINE=InnoDB`)
 if err != nil {
  return fmt.Errorf("ensuring dbtools_migration_history schema: %w", err)
 }
 // Column added by dbtools builds before content hashing existed.
 // MySQL's "ADD COLUMN IF NOT EXISTS" is version-gated (8.0.29+), so
 // check information_schema first for broader compatibility — same
 // approach as sqliteengine's pragma_table_info check.
 cols, err := db.Query(`
SELECT COLUMN_NAME FROM information_schema.columns
WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'dbtools_migration_history' AND COLUMN_NAME = 'content_sha256'`)
 if err != nil {
  return fmt.Errorf("inspecting dbtools_migration_history columns: %w", err)
 }
 defer cols.Close()
 if !cols.Next() {
  if _, err := db.Exec(`ALTER TABLE dbtools_migration_history ADD COLUMN content_sha256 CHAR(64) NULL`); err != nil {
   return fmt.Errorf("adding content_sha256 to dbtools_migration_history: %w", err)
  }
 }
 return nil
}

func checkVersionRange(version uint64) error {
 if version > math.MaxInt64 {
  return fmt.Errorf("migration version %d exceeds the ledger's BIGINT range", version)
 }
 return nil
}

func (mysqlLedgerStore) backfill(db ledger.DBTX, currentVersion uint64, hasVersion bool, allVersions []uint64) error {
 if !hasVersion {
  return nil
 }
 for _, v := range allVersions {
  if v > currentVersion {
   continue
  }
  if err := checkVersionRange(v); err != nil {
   return err
  }
  _, err := db.Exec(`
INSERT IGNORE INTO dbtools_migration_history (version, status, recorded_at, note)
VALUES (?, 'applied', NULL, 'backfilled: applied before ledger existed')`, int64(v))
  if err != nil {
   return fmt.Errorf("backfilling version %d: %w", v, err)
  }
 }
 return nil
}

// SetStatus upserts version's ledger row, preserving content_sha256 on
// update — the ON DUPLICATE KEY UPDATE clause deliberately omits it.
func (mysqlLedgerStore) SetStatus(db ledger.DBTX, version uint64, status ledger.Status, note string) error {
 if err := checkVersionRange(version); err != nil {
  return err
 }
 _, err := db.Exec(`
INSERT INTO dbtools_migration_history (version, status, recorded_at, note)
VALUES (?, ?, NOW(), ?)
ON DUPLICATE KEY UPDATE status = VALUES(status), recorded_at = VALUES(recorded_at), note = VALUES(note)`,
  int64(version), string(status), note)
 if err != nil {
  return fmt.Errorf("setting status for version %d: %w", version, err)
 }
 return nil
}

// SetStatusWithHash is SetStatus plus recording the applied migration
// file's content hash, so verify can detect edits after apply.
func (mysqlLedgerStore) SetStatusWithHash(db ledger.DBTX, version uint64, status ledger.Status, note, contentHash string) error {
 if err := checkVersionRange(version); err != nil {
  return err
 }
 _, err := db.Exec(`
INSERT INTO dbtools_migration_history (version, status, recorded_at, note, content_sha256)
VALUES (?, ?, NOW(), ?, ?)
ON DUPLICATE KEY UPDATE status = VALUES(status), recorded_at = VALUES(recorded_at), note = VALUES(note), content_sha256 = VALUES(content_sha256)`,
  int64(version), string(status), note, contentHash)
 if err != nil {
  return fmt.Errorf("setting status for version %d: %w", version, err)
 }
 return nil
}

// List returns every ledger row, ordered by version ascending.
func (mysqlLedgerStore) List(db ledger.DBTX) ([]ledger.Entry, error) {
 rows, err := db.Query(`SELECT version, status, recorded_at, note, content_sha256 FROM dbtools_migration_history ORDER BY version ASC`)
 if err != nil {
  return nil, fmt.Errorf("listing ledger: %w", err)
 }
 defer rows.Close()

 var entries []ledger.Entry
 for rows.Next() {
  var e ledger.Entry
  var version int64
  var status string
  var recordedAt sql.NullTime
  var note, contentHash sql.NullString
  if err := rows.Scan(&version, &status, &recordedAt, &note, &contentHash); err != nil {
   return nil, fmt.Errorf("scanning ledger row: %w", err)
  }
  if version < 0 {
   return nil, fmt.Errorf("ledger contains negative version %d; the table was modified outside dbtools", version)
  }
  e.Version = uint64(version)
  e.Status = ledger.Status(status)
  if recordedAt.Valid {
   t := recordedAt.Time
   e.RecordedAt = &t
  }
  e.Note = note.String
  e.ContentSHA256 = contentHash.String
  entries = append(entries, e)
 }
 return entries, rows.Err()
}

// AppliedVersions returns every version currently marked "applied", ascending.
func (s mysqlLedgerStore) AppliedVersions(db ledger.DBTX) ([]uint64, error) {
 entries, err := s.List(db)
 if err != nil {
  return nil, err
 }
 var versions []uint64
 for _, e := range entries {
  if e.Status == ledger.StatusApplied {
   versions = append(versions, e.Version)
  }
 }
 return versions, nil
}

// Sync ensures MySQL db's ledger table exists and is backfilled. Refuses
// to backfill when the cursor is dirty (a previous apply failed partway).
func (s mysqlLedgerStore) Sync(db *sql.DB, m *migrator.Migrator, migrationsDir string) error {
 if err := s.ensureSchema(db); err != nil {
  return err
 }
 version, dirty, hasVersion, err := m.Version()
 if err != nil {
  return err
 }
 if dirty {
  return fmt.Errorf("migration cursor is dirty (a previous apply failed partway through version %d); run `dbtools repair <target>` to resolve it before syncing the ledger", version)
 }
 allVersions, err := migrator.ListVersions(migrationsDir)
 if err != nil {
  return err
 }
 return s.backfill(db, version, hasVersion, allVersions)
}
```

- [ ] **Step 2: Write the integration test**

```go
//go:build integration

package mysqlengine

import (
 "os"
 "testing"

 "github.com/seanpham99/dbtools/internal/ledger"
)

func openTestDB(t *testing.T) *sql.DB {
 t.Helper()
 url := os.Getenv("DBTOOLS_TEST_MYSQL_URL")
 if url == "" {
  t.Skip("DBTOOLS_TEST_MYSQL_URL not set, skipping integration test")
 }
 db, err := Open(url)
 if err != nil {
  t.Fatalf("Open() returned error: %v", err)
 }
 t.Cleanup(func() {
  db.Exec(`DROP TABLE IF EXISTS dbtools_migration_history`)
  db.Close()
 })
 return db
}

func TestEnsureSchema_Idempotent(t *testing.T) {
 db := openTestDB(t)
 store := mysqlLedgerStore{}
 if err := store.ensureSchema(db); err != nil {
  t.Fatalf("ensureSchema() returned error: %v", err)
 }
 if err := store.ensureSchema(db); err != nil {
  t.Fatalf("second ensureSchema() returned error: %v", err)
 }
}

func TestSetStatusInsertsAndUpdates(t *testing.T) {
 db := openTestDB(t)
 store := mysqlLedgerStore{}
 if err := store.ensureSchema(db); err != nil {
  t.Fatal(err)
 }

 if err := store.SetStatus(db, 1, ledger.StatusApplied, "first"); err != nil {
  t.Fatalf("SetStatus(insert) returned error: %v", err)
 }
 entries, err := store.List(db)
 if err != nil {
  t.Fatal(err)
 }
 if len(entries) != 1 || entries[0].Status != ledger.StatusApplied || entries[0].RecordedAt == nil {
  t.Fatalf("after insert: entries = %+v, want one applied row with non-nil RecordedAt", entries)
 }

 if err := store.SetStatus(db, 1, ledger.StatusReverted, "second"); err != nil {
  t.Fatalf("SetStatus(update) returned error: %v", err)
 }
 entries, err = store.List(db)
 if err != nil {
  t.Fatal(err)
 }
 if len(entries) != 1 || entries[0].Status != ledger.StatusReverted || entries[0].Note != "second" {
  t.Fatalf("after update: entries = %+v, want one reverted row noted 'second'", entries)
 }
}

func TestSetStatusWithHashPreservesHashOnPlainUpdate(t *testing.T) {
 db := openTestDB(t)
 store := mysqlLedgerStore{}
 if err := store.ensureSchema(db); err != nil {
  t.Fatal(err)
 }
 if err := store.SetStatusWithHash(db, 1, ledger.StatusApplied, "with hash", "deadbeef"); err != nil {
  t.Fatal(err)
 }
 if err := store.SetStatus(db, 1, ledger.StatusApplied, "touched again"); err != nil {
  t.Fatal(err)
 }
 entries, err := store.List(db)
 if err != nil {
  t.Fatal(err)
 }
 if len(entries) != 1 || entries[0].ContentSHA256 != "deadbeef" {
  t.Fatalf("entries = %+v, want content_sha256 preserved across a plain SetStatus", entries)
 }
}

func TestAppliedVersions(t *testing.T) {
 db := openTestDB(t)
 store := mysqlLedgerStore{}
 if err := store.ensureSchema(db); err != nil {
  t.Fatal(err)
 }
 if err := store.SetStatus(db, 1, ledger.StatusApplied, ""); err != nil {
  t.Fatal(err)
 }
 if err := store.SetStatus(db, 2, ledger.StatusReverted, ""); err != nil {
  t.Fatal(err)
 }
 if err := store.SetStatus(db, 3, ledger.StatusApplied, ""); err != nil {
  t.Fatal(err)
 }
 versions, err := store.AppliedVersions(db)
 if err != nil {
  t.Fatal(err)
 }
 want := []uint64{1, 3}
 if len(versions) != len(want) || versions[0] != want[0] || versions[1] != want[1] {
  t.Errorf("AppliedVersions() = %v, want %v", versions, want)
 }
}
```

Note the `openTestDB` helper needs `"database/sql"` in its import block — add it alongside `"os"` and `"testing"`.

- [ ] **Step 3: Run the full package test suite (unit only — integration needs a live server, run later in Task 8)**

Run: `go build ./... && go test ./internal/engine/mysqlengine/... -v`
Expected: `mysqlengine.go` (Task 3) now compiles; `TestMySQLRegistered`, `TestForTargetValidatesConfiguredEngine` (Task 3), all `TestExtractObjects_*`/`TestExtractDroppedObjects_*`/`TestExistsRejectsUnknownKind` (Task 4), and `TestDSNFromURL_*` (Task 2) all PASS. The `integration`-tagged file in this task is skipped by a plain `go test` (no build tag set).

- [ ] **Step 4: Commit**

```bash
git add internal/engine/mysqlengine/
git commit -m "feat(mysqlengine): ledger dialect, completing the engine seam"
```

---

### Task 6: Introspection (`generate` support)

**Files:**

- Create: `internal/engine/mysqlengine/introspect.go`
- Create: `internal/engine/mysqlengine/introspect_test.go`

**Interfaces:**

- Consumes: `generate.TableSchema`, `generate.ColumnSchema`, `generate.SanitizeFieldName` from `internal/generate`.
- Produces: `func introspect(db *sql.DB, excludeList []string) ([]generate.TableSchema, []string, error)` (used by Task 3's `MySQL.Introspect`); `func MapMySQLToPython(dataType string) (string, bool)`, exported so its unit test — and any future caller — doesn't need a database.

- [ ] **Step 1: Write the failing test (pure function, no DB)**

```go
package mysqlengine

import "testing"

func TestMapMySQLToPython(t *testing.T) {
 tests := []struct {
  input      string
  expected   string
  expectKnow bool
 }{
  {"int", "int", true},
  {"BIGINT", "int", true},
  {"tinyint", "int", true},
  {"decimal", "Decimal", true},
  {"double", "float", true},
  {"varchar", "str", true},
  {"enum", "str", true},
  {"datetime", "datetime", true},
  {"timestamp", "datetime", true},
  {"time", "time", true},
  {"blob", "bytes", true},
  {"json", "Any", true},
  {"geometry", "Any", false},
 }
 for _, tt := range tests {
  actual, known := MapMySQLToPython(tt.input)
  if actual != tt.expected {
   t.Errorf("MapMySQLToPython(%q) = %q; want %q", tt.input, actual, tt.expected)
  }
  if known != tt.expectKnow {
   t.Errorf("MapMySQLToPython(%q) known = %v; want %v", tt.input, known, tt.expectKnow)
  }
 }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/mysqlengine/... -run TestMapMySQLToPython -v`
Expected: FAIL — `MapMySQLToPython` undefined.

- [ ] **Step 3: Write the implementation**

```go
package mysqlengine

import (
 "database/sql"
 "fmt"
 "strings"

 "github.com/seanpham99/dbtools/internal/generate"
)

// MapMySQLToPython maps a MySQL DATA_TYPE (as reported by
// information_schema.columns) to a Python type string. The second return
// value is false when dataType has no known mapping and fell back to "Any".
func MapMySQLToPython(dataType string) (string, bool) {
 switch strings.ToLower(dataType) {
 case "tinyint", "smallint", "mediumint", "int", "bigint", "year":
  return "int", true
 case "float", "double":
  return "float", true
 case "decimal", "numeric":
  return "Decimal", true
 case "char", "varchar", "text", "tinytext", "mediumtext", "longtext", "enum", "set":
  return "str", true
 case "date", "datetime", "timestamp":
  return "datetime", true
 case "time":
  return "time", true
 case "binary", "varbinary", "blob", "tinyblob", "mediumblob", "longblob":
  return "bytes", true
 case "json":
  return "Any", true
 default:
  return "Any", false
 }
}

// introspect queries information_schema for the current database's base
// tables, excluding excludeList — the same contract as the other engines'
// Introspect. MySQL has no separate schema layer (TABLE_SCHEMA = the
// current database), so unlike mssqlengine/postgresengine there is no
// cross-schema enumeration to do.
func introspect(db *sql.DB, excludeList []string) ([]generate.TableSchema, []string, error) {
 excludeSet := make(map[string]bool)
 for _, e := range excludeList {
  excludeSet[strings.ToLower(strings.TrimSpace(e))] = true
 }

 query := `
  SELECT
   c.TABLE_SCHEMA,
   c.TABLE_NAME,
   c.COLUMN_NAME,
   c.DATA_TYPE,
   c.IS_NULLABLE,
   c.CHARACTER_MAXIMUM_LENGTH,
   c.NUMERIC_PRECISION,
   c.NUMERIC_SCALE
  FROM information_schema.tables t
  JOIN information_schema.columns c
   ON t.TABLE_SCHEMA = c.TABLE_SCHEMA AND t.TABLE_NAME = c.TABLE_NAME
  WHERE t.TABLE_SCHEMA = DATABASE() AND t.TABLE_TYPE = 'BASE TABLE'
  ORDER BY c.TABLE_NAME, c.ORDINAL_POSITION
 `

 rows, err := db.Query(query)
 if err != nil {
  return nil, nil, fmt.Errorf("introspecting schema: %w", err)
 }
 defer rows.Close()

 tableMap := make(map[string]*generate.TableSchema)
 var tableOrder []string
 var unmapped []string

 for rows.Next() {
  var schemaName, tableName, colName, dataType, isNullableStr string
  var maxLen, precision, scale sql.NullInt64

  if err := rows.Scan(&schemaName, &tableName, &colName, &dataType, &isNullableStr, &maxLen, &precision, &scale); err != nil {
   return nil, nil, fmt.Errorf("scanning column info: %w", err)
  }

  if excludeSet[strings.ToLower(tableName)] {
   continue
  }

  tbl, exists := tableMap[tableName]
  if !exists {
   tbl = &generate.TableSchema{Schema: schemaName, Name: tableName}
   tableMap[tableName] = tbl
   tableOrder = append(tableOrder, tableName)
  }

  pythonType, known := MapMySQLToPython(dataType)
  if !known {
   unmapped = append(unmapped, fmt.Sprintf("%s.%s: %s", tableName, colName, dataType))
  }

  tbl.Columns = append(tbl.Columns, generate.ColumnSchema{
   Name:       colName,
   PyName:     generate.SanitizeFieldName(colName),
   DataType:   dataType,
   PythonType: pythonType,
   IsNullable: strings.ToUpper(isNullableStr) == "YES",
   MaxLength:  maxLen,
   Precision:  precision,
   Scale:      scale,
  })
 }

 if err := rows.Err(); err != nil {
  return nil, nil, fmt.Errorf("iterating schema rows: %w", err)
 }

 result := make([]generate.TableSchema, 0, len(tableOrder))
 for _, name := range tableOrder {
  result = append(result, *tableMap[name])
 }
 return result, unmapped, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/mysqlengine/... -run TestMapMySQLToPython -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/engine/mysqlengine/introspect.go internal/engine/mysqlengine/introspect_test.go
git commit -m "feat(mysqlengine): live-schema introspection for generate"
```

---

### Task 7: `EnsureDatabase` support + wire the engine into the binary

**Files:**

- Modify: `internal/engine/ensure.go`
- Modify: `cmd/root.go`
- Create: `cmd/mysql_engine_registered_test.go`

**Interfaces:**

- Consumes: `mysql.ParseDSN`, `mysql.Config` from `github.com/go-sql-driver/mysql` (already a direct dependency after Task 2).
- Produces: `EnsureDatabase` handles `eng.Name() == "mysql"`; `cmd/root.go`'s blank imports register `mysqlengine` for every `dbtools` invocation, exactly like the other three engines.

- [ ] **Step 1: Add the failing assertion first**

```go
package cmd

import (
 "testing"

 "github.com/seanpham99/dbtools/internal/engine"
 _ "github.com/seanpham99/dbtools/internal/engine/mssqlengine"
 _ "github.com/seanpham99/dbtools/internal/engine/postgresengine"
 _ "github.com/seanpham99/dbtools/internal/engine/sqliteengine"
)

// This package (cmd) is what actually wires every engine into the dbtools
// binary via its blank imports in root.go. This test asserts mysql is
// among them without depending on root.go's import list directly (so it
// still fails clearly if the import is missing or removed later).
func TestMySQLEngineRegisteredInBinary(t *testing.T) {
 names := engine.Names()
 for _, n := range names {
  if n == "mysql" {
   return
  }
 }
 t.Fatalf("engine.Names() = %v, want it to include \"mysql\" (is internal/engine/mysqlengine imported from cmd/root.go?)", names)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/... -run TestMySQLEngineRegisteredInBinary -v`
Expected: FAIL — `engine.Names()` doesn't include `"mysql"` yet (mssql/postgres/sqlite are registered by this test file's own blank imports, but mysql isn't imported anywhere yet).

- [ ] **Step 3: Wire the blank import in `cmd/root.go`**

In `cmd/root.go`, the existing import block is:

```go
import (
 "fmt"
 "os"

 // Engine implementations self-register with internal/engine in their
 // package init(); every registered engine's commands work from here.
 _ "github.com/seanpham99/dbtools/internal/engine/mssqlengine"
 _ "github.com/seanpham99/dbtools/internal/engine/postgresengine"
 _ "github.com/seanpham99/dbtools/internal/engine/sqliteengine"
 "github.com/seanpham99/dbtools/internal/localenv"
 "github.com/spf13/cobra"
)
```

Add the mysql line, keeping the block alphabetized like its neighbors:

```go
import (
 "fmt"
 "os"

 // Engine implementations self-register with internal/engine in their
 // package init(); every registered engine's commands work from here.
 _ "github.com/seanpham99/dbtools/internal/engine/mssqlengine"
 _ "github.com/seanpham99/dbtools/internal/engine/mysqlengine"
 _ "github.com/seanpham99/dbtools/internal/engine/postgresengine"
 _ "github.com/seanpham99/dbtools/internal/engine/sqliteengine"
 "github.com/seanpham99/dbtools/internal/localenv"
 "github.com/spf13/cobra"
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/... -run TestMySQLEngineRegisteredInBinary -v`
Expected: PASS.

- [ ] **Step 5: Add `EnsureDatabase` support in `internal/engine/ensure.go`**

The existing switch in `EnsureDatabase`:

```go
 switch eng.Name() {
 case "sqlite":
  return ensureSQLitePath(rawURL)
 case "postgres":
  return ensurePostgresDatabase(rawURL)
 case "mssql":
  return ensureMSSQLDatabase(rawURL)
 default:
  return nil
 }
```

becomes:

```go
 switch eng.Name() {
 case "sqlite":
  return ensureSQLitePath(rawURL)
 case "postgres":
  return ensurePostgresDatabase(rawURL)
 case "mssql":
  return ensureMSSQLDatabase(rawURL)
 case "mysql":
  return ensureMySQLDatabase(rawURL)
 default:
  return nil
 }
```

Add the import (alongside the existing `_ "github.com/lib/pq"` / `_ "github.com/microsoft/go-mssqldb"` blank imports at the top of the file) and the new function at the bottom of the file:

```go
 "github.com/go-sql-driver/mysql"
```

```go
func ensureMySQLDatabase(rawURL string) error {
 cfg, err := mysql.ParseDSN(strings.TrimPrefix(rawURL, "mysql://"))
 if err != nil {
  return nil
 }
 dbName := cfg.DBName
 if dbName == "" {
  return nil
 }
 cfg.ParseTime = true

 // First test if target DB already exists and is reachable.
 testDB, err := sql.Open("mysql", cfg.FormatDSN())
 if err == nil {
  if pingErr := testDB.Ping(); pingErr == nil {
   testDB.Close()
   return nil
  }
  testDB.Close()
 }

 // Connect without a default database to run the administrative CREATE.
 adminCfg := *cfg
 adminCfg.DBName = ""
 mainDB, err := sql.Open("mysql", adminCfg.FormatDSN())
 if err != nil {
  return nil
 }
 defer mainDB.Close()

 if err := mainDB.Ping(); err != nil {
  return nil
 }

 safeName := strings.ReplaceAll(dbName, "`", "``")
 if _, err := mainDB.Exec(fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`", safeName)); err != nil {
  return fmt.Errorf("creating mysql database %q: %w", dbName, err)
 }
 return nil
}
```

`sql.Open("mysql", ...)` here registers no new driver import cycle issue: the `github.com/go-sql-driver/mysql` package registers itself as the `"mysql"` `database/sql` driver in its own `init()`, and this file now imports it directly (not blank) to use `mysql.ParseDSN`/`mysql.Config`, which triggers that `init()` all the same.

- [ ] **Step 6: Verify the whole binary builds**

Run: `go build ./... && go vet ./...`
Expected: succeeds.

- [ ] **Step 7: Commit**

```bash
git add cmd/root.go cmd/mysql_engine_registered_test.go internal/engine/ensure.go
git commit -m "feat: wire mysql engine into the dbtools binary and EnsureDatabase"
```

---

### Task 8: classicmodels MySQL fixtures + integration asset suite

**Files:**

- Create: `internal/testutil/testdata/classicmodels/mysql/20260822000001_offices_employees.up.sql`
- Create: `internal/testutil/testdata/classicmodels/mysql/20260822000001_offices_employees.down.sql`
- Create: `internal/testutil/testdata/classicmodels/mysql/20260822000002_products.up.sql`
- Create: `internal/testutil/testdata/classicmodels/mysql/20260822000002_products.down.sql`
- Create: `internal/testutil/testdata/classicmodels/mysql/20260822000003_customers.up.sql`
- Create: `internal/testutil/testdata/classicmodels/mysql/20260822000003_customers.down.sql`
- Create: `internal/testutil/testdata/classicmodels/mysql/20260822000004_orders_payments.up.sql`
- Create: `internal/testutil/testdata/classicmodels/mysql/20260822000004_orders_payments.down.sql`
- Create: `internal/engine/mysqlengine/assets_integration_test.go`
- Modify: `internal/testutil/testdata/classicmodels/README.md` (mention the new `mysql/` dialect dir)
- Modify (generated, not hand-written): `internal/testutil/testdata/golden/mysql/models.py`, `internal/testutil/testdata/golden/mysql/models.ts`

**Interfaces:**

- Consumes: `testutil.RunAssets(t, dialect, rawURL string)` from `internal/testutil` — already generic across dialects, no changes needed there.
- Produces: nothing new exported; this task proves the whole engine end-to-end against a real MySQL server via the existing shared test harness.

The table schema below is transcribed from `internal/testutil/testdata/golden/sqlite/models.py` (the source of truth for column names/nullability/types across all three existing dialects) and translated to MySQL DDL with real foreign keys (MySQL's InnoDB engine supports them natively, unlike the other three dialects' migrations having no FK enforcement requirement — this plan adds them since MySQL's classicmodels heritage is the canonical MySQL sample DB and normally ships with FKs; **decision:** keep them for realism, `testutil.RunAssets` doesn't assert their presence either way).

- [ ] **Step 1: Write `20260822000001_offices_employees.up.sql`**

```sql
CREATE TABLE offices (
    officeCode VARCHAR(10) NOT NULL PRIMARY KEY,
    city VARCHAR(50) NOT NULL,
    phone VARCHAR(50) NOT NULL,
    addressLine1 VARCHAR(50) NOT NULL,
    addressLine2 VARCHAR(50) NULL,
    state VARCHAR(50) NULL,
    country VARCHAR(50) NOT NULL,
    postalCode VARCHAR(15) NOT NULL,
    territory VARCHAR(10) NOT NULL
) ENGINE=InnoDB;

CREATE TABLE employees (
    employeeNumber INT NOT NULL PRIMARY KEY,
    lastName VARCHAR(50) NOT NULL,
    firstName VARCHAR(50) NOT NULL,
    extension VARCHAR(10) NOT NULL,
    email VARCHAR(100) NOT NULL,
    officeCode VARCHAR(10) NOT NULL,
    reportsTo INT NULL,
    jobTitle VARCHAR(50) NOT NULL,
    CONSTRAINT fk_employees_office FOREIGN KEY (officeCode) REFERENCES offices (officeCode),
    CONSTRAINT fk_employees_reportsto FOREIGN KEY (reportsTo) REFERENCES employees (employeeNumber)
) ENGINE=InnoDB;
```

- [ ] **Step 2: Write `20260822000001_offices_employees.down.sql`**

```sql
DROP TABLE IF EXISTS employees;
DROP TABLE IF EXISTS offices;
```

- [ ] **Step 3: Write `20260822000002_products.up.sql`**

```sql
CREATE TABLE productlines (
    productLine VARCHAR(50) NOT NULL PRIMARY KEY,
    textDescription VARCHAR(4000) NULL,
    htmlDescription MEDIUMTEXT NULL,
    image MEDIUMBLOB NULL
) ENGINE=InnoDB;

CREATE TABLE products (
    productCode VARCHAR(15) NOT NULL PRIMARY KEY,
    productName VARCHAR(70) NOT NULL,
    productLine VARCHAR(50) NOT NULL,
    productScale VARCHAR(10) NOT NULL,
    productVendor VARCHAR(50) NOT NULL,
    productDescription TEXT NOT NULL,
    quantityInStock INT NOT NULL,
    buyPrice DECIMAL(10,2) NOT NULL,
    MSRP DECIMAL(10,2) NOT NULL,
    CONSTRAINT fk_products_productline FOREIGN KEY (productLine) REFERENCES productlines (productLine)
) ENGINE=InnoDB;
```

- [ ] **Step 4: Write `20260822000002_products.down.sql`**

```sql
DROP TABLE IF EXISTS products;
DROP TABLE IF EXISTS productlines;
```

- [ ] **Step 5: Write `20260822000003_customers.up.sql`**

```sql
CREATE TABLE customers (
    customerNumber INT NOT NULL PRIMARY KEY,
    customerName VARCHAR(50) NOT NULL,
    contactLastName VARCHAR(50) NOT NULL,
    contactFirstName VARCHAR(50) NOT NULL,
    phone VARCHAR(50) NOT NULL,
    addressLine1 VARCHAR(50) NOT NULL,
    addressLine2 VARCHAR(50) NULL,
    city VARCHAR(50) NOT NULL,
    state VARCHAR(50) NULL,
    postalCode VARCHAR(15) NULL,
    country VARCHAR(50) NOT NULL,
    salesRepEmployeeNumber INT NULL,
    creditLimit DECIMAL(10,2) NULL,
    CONSTRAINT fk_customers_salesrep FOREIGN KEY (salesRepEmployeeNumber) REFERENCES employees (employeeNumber)
) ENGINE=InnoDB;
```

- [ ] **Step 6: Write `20260822000003_customers.down.sql`**

```sql
DROP TABLE IF EXISTS customers;
```

- [ ] **Step 7: Write `20260822000004_orders_payments.up.sql`**

```sql
CREATE TABLE orders (
    orderNumber INT NOT NULL PRIMARY KEY,
    orderDate DATETIME NOT NULL,
    requiredDate DATETIME NOT NULL,
    shippedDate DATETIME NULL,
    status VARCHAR(15) NOT NULL,
    comments TEXT NULL,
    customerNumber INT NOT NULL,
    CONSTRAINT fk_orders_customer FOREIGN KEY (customerNumber) REFERENCES customers (customerNumber)
) ENGINE=InnoDB;

CREATE TABLE orderdetails (
    orderNumber INT NOT NULL,
    productCode VARCHAR(15) NOT NULL,
    quantityOrdered INT NOT NULL,
    priceEach DECIMAL(10,2) NOT NULL,
    orderLineNumber SMALLINT NOT NULL,
    PRIMARY KEY (orderNumber, productCode),
    CONSTRAINT fk_orderdetails_order FOREIGN KEY (orderNumber) REFERENCES orders (orderNumber),
    CONSTRAINT fk_orderdetails_product FOREIGN KEY (productCode) REFERENCES products (productCode)
) ENGINE=InnoDB;

CREATE TABLE payments (
    customerNumber INT NOT NULL,
    checkNumber VARCHAR(50) NOT NULL,
    paymentDate DATETIME NOT NULL,
    amount DECIMAL(10,2) NOT NULL,
    PRIMARY KEY (customerNumber, checkNumber),
    CONSTRAINT fk_payments_customer FOREIGN KEY (customerNumber) REFERENCES customers (customerNumber)
) ENGINE=InnoDB;
```

- [ ] **Step 8: Write `20260822000004_orders_payments.down.sql`**

```sql
DROP TABLE IF EXISTS payments;
DROP TABLE IF EXISTS orderdetails;
DROP TABLE IF EXISTS orders;
```

- [ ] **Step 9: Add the integration asset test**

```go
//go:build integration

package mysqlengine

import (
 "os"
 "testing"

 "github.com/seanpham99/dbtools/internal/testutil"
)

func TestIntegrationAssets(t *testing.T) {
 rawURL := os.Getenv("DBTOOLS_TEST_MYSQL_URL")
 if rawURL == "" {
  t.Skip("DBTOOLS_TEST_MYSQL_URL not set, skipping integration test")
 }
 testutil.RunAssets(t, "mysql", rawURL)
}
```

- [ ] **Step 10: Run against a local MySQL to generate the golden files**

This requires a real MySQL server. If Docker is available locally:

```bash
docker run -d --name dbtools-mysql-devplan -e MYSQL_ROOT_PASSWORD=dbtools -e MYSQL_DATABASE=dbtools_test -p 3306:3306 mysql:8.0
# wait for it to accept connections
until docker exec dbtools-mysql-devplan mysqladmin ping -proot -h localhost --silent; do sleep 2; done

export DBTOOLS_TEST_MYSQL_URL="mysql://root:dbtools@tcp(127.0.0.1:3306)/dbtools_test?parseTime=true"
DBTOOLS_TEST_UPDATE=1 go test -tags=integration ./internal/engine/mysqlengine/... -run TestIntegrationAssets -v

docker rm -f dbtools-mysql-devplan
```

Expected: `internal/testutil/testdata/golden/mysql/models.py` and `models.ts` are written (this is the repo's own documented convention for refreshing goldens — see `internal/testutil/runner.go`'s `DBTOOLS_TEST_UPDATE` check — not a fabricated step). Re-run the same command **without** `DBTOOLS_TEST_UPDATE=1` afterward to confirm `TestIntegrationAssets` now PASSes against the freshly-written goldens.

- [ ] **Step 11: Update the fixture README**

Add one line to `internal/testutil/testdata/classicmodels/README.md`'s "Dialect Migration Sequence" section noting `mysql/` joins `sqlite/`, `postgres/`, `mssql/`:

```markdown
## Dialect Migration Sequence
Each dialect directory (`sqlite/`, `postgres/`, `mssql/`, `mysql/`) provides a 4-step migration sequence with matching `.up.sql` and `.down.sql` scripts:
```

- [ ] **Step 12: Commit**

```bash
git add internal/testutil/testdata/classicmodels/mysql/ internal/testutil/testdata/classicmodels/README.md internal/testutil/testdata/golden/mysql/ internal/engine/mysqlengine/assets_integration_test.go
git commit -m "test(mysqlengine): classicmodels fixture corpus and golden typegen"
```

---

### Task 9: CI wiring and docs

**Files:**

- Modify: `.github/workflows/ci.yml`
- Modify: `cmd/init.go`
- Modify: `README.md`
- Modify: `docs/roadmap.md`

**Interfaces:** none — this task is CI config and documentation only.

- [ ] **Step 1: Add `mysql` to the integration matrix in `.github/workflows/ci.yml`**

Change:

```yaml
      matrix:
        db: [mssql, postgres, sqlite]
```

to:

```yaml
      matrix:
        db: [mssql, postgres, sqlite, mysql]
```

Add a `mysql` service alongside the existing `mssql`/`postgres` services (the official `mysql` image auto-creates `MYSQL_DATABASE` on first boot, same as the `postgres` service — no manual "create database" step like the MSSQL job needs):

```yaml
      mysql:
        image: mysql:8.0
        env:
          MYSQL_ROOT_PASSWORD: dbtools
          MYSQL_DATABASE: dbtools_test
        ports:
          - 3306:3306
        options: >-
          --health-cmd "mysqladmin ping -proot -h localhost"
          --health-interval 10s
          --health-timeout 5s
          --health-retries 10
```

Add the `DBTOOLS_TEST_MYSQL_URL` env line next to the existing `DBTOOLS_TEST_POSTGRES_URL`/`DBTOOLS_TEST_SQLITE_URL` ones in the "Integration tests" step:

```yaml
          DBTOOLS_TEST_MYSQL_URL: ${{ matrix.db == 'mysql' && 'mysql://root:dbtools@tcp(localhost:3306)/dbtools_test?parseTime=true' || '' }}
```

Add a `mysql)` case to the `case "${{ matrix.db }}" in` dispatch, mirroring the `sqlite)` branch's minimal scope (no shared `internal/verify`/`internal/apply` integration files exist for MySQL, matching the scope decision in Task 8 — only `sqlite)` runs this narrow, and this plan follows that precedent rather than mssql's/postgres's broader runs):

```yaml
            mysql)
              go test -tags=integration ./internal/engine/mysqlengine/... -count=1
              ;;
```

The root-cause password note from the Global Constraints section applies here: `dbtools`/`root` is a throwaway value scoped to this ephemeral CI container, not a secret — matches the existing `postgres` service's plaintext `dbtools`/`dbtools` credentials, not the `mssql` service's `secrets.DBTOOLS_TEST_MSSQL_SA_PASSWORD` (MSSQL's `MSSQL_SA_PASSWORD` has a minimum-complexity requirement enforced at container start, which is the actual reason that one uses a managed secret — not a hard rule this plan needs to follow for MySQL, which enforces no such policy on `MYSQL_ROOT_PASSWORD`).

- [ ] **Step 2: Update the `dbtools init` config template's engine list**

In `cmd/init.go`, `defaultConfigTemplate`'s comment currently reads:

```go
// Supported engines: mssql, postgres, sqlite.
```

Change to:

```go
// Supported engines: mssql, postgres, sqlite, mysql.
```

- [ ] **Step 3: Verify no test asserts the old string**

Run: `go test ./cmd/... -run TestRunInit -v`
Expected: PASS (`cmd/init_test.go`'s two tests check file creation and non-overwrite behavior, not the exact template contents — confirmed by reading the file: neither test does a substring match against `defaultConfigTemplate`).

- [ ] **Step 4: Update `README.md`**

In the "Key Features" section, the "Multi-Engine Support" bullet currently reads:

```markdown
- **Multi-Engine Support**: Native migration engines for SQL Server (MSSQL), PostgreSQL (with session reset isolation), and SQLite (file-based).
```

Change to:

```markdown
- **Multi-Engine Support**: Native migration engines for SQL Server (MSSQL), PostgreSQL (with session reset isolation), MySQL, and SQLite (file-based).
```

In the "Quick Start" section's example `dbtools.toml`, the engine comment:

```toml
engine = "sqlite" # sqlite, postgres, or mssql
```

becomes:

```toml
engine = "sqlite" # sqlite, postgres, mssql, or mysql
```

- [ ] **Step 5: Update `docs/roadmap.md`**

The phases table row:

```markdown
| v0.3/4 | **MySQL engine** | Add MySQL as a supported migration engine (golang-migrate has a mysql driver). classicmodels fixtures are MySQL-native — natural fit. Scheduled after v0.3 core. |
```

becomes:

```markdown
| v0.3/4 ✅ | **MySQL engine** | Shipped: `internal/engine/mysqlengine`, mirroring the mssql/postgres/sqlite seam. classicmodels fixtures ported to MySQL with real FKs. Scope: TABLE/VIEW DDL detection only (no stored procedures — see the implementation plan for why); `internal/container` local-dev support not included. |
```

- [ ] **Step 6: Run the full local verification pass**

```bash
scripts/dev-local.sh all
go vet ./...
gofmt -l .
```

Expected: all green, no unformatted files, no vet warnings.

- [ ] **Step 7: Commit**

```bash
git add .github/workflows/ci.yml cmd/init.go README.md docs/roadmap.md
git commit -m "docs+ci: wire mysql into the integration matrix and public docs"
```

---

## Self-review notes (for whoever executes this plan)

- **Spec coverage:** roadmap's MySQL entry ("Add MySQL as a supported migration engine... classicmodels fixtures are MySQL-native — natural fit") is covered by Tasks 1–8; the CI/docs half of "shipped" is Task 9.
- **Scope calls made explicitly, not left as TBD:** DDL scope (TABLE/VIEW only, Task 4's header), `Schema=""` sentinel for MySQL's lack of a schema layer (Task 4), `internal/container` excluded (Global Constraints), CI job scoped narrowly like `sqlite` rather than broadly like `mssql`/`postgres` (Task 9).
- **Type/signature consistency check:** `mysqlLedgerStore` methods match the `engine.LedgerStore` interface signatures exactly (`Sync(db *sql.DB, ...)`, others taking `ledger.DBTX`) — same split MSSQL/Postgres/SQLite use, where `Sync` needs the concrete `*sql.DB` (it opens no transaction) while the rest accept the narrower `ledger.DBTX` (so `repair.Run` can pass a `*sql.Tx`).
- **One known follow-up not in this plan:** a `dbtools start`/`stop` MySQL container spec in `internal/container` (deliberately deferred — see Global Constraints).
