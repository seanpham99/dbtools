# JSON Contract & Connection/Error Polish — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Settle the `--json` output contract (#98/#99/#100) and polish first-run connection/refusal UX (#97/#102), plus the squashed-history adopt gap (#101), with tests green and docs updated.

**Architecture:** The three JSON issues are one root cause — no stated `--json` contract. Fix: `status`/`plan` drop `omitempty` on state fields (#98), `up` folds the job summary into its single status document and removes the second JSON line (#99), and the contract is documented once in `docs/exit-codes.md` with a schema validation rule for consumers (#100). `adopt` gets a `--allow-orphans-before <version>` targeted gate (#101) alongside `--force`. Postgres gets an SSL-hint diagnostic on the 42501-style pattern (#102). Command polish: `up`/`push`/`down` silence usage on refusals; `repair`/`rollback` drop the dead "cursor" wording; `EnsureSchema` notice suppression. Each lands behind unit tests; `scripts/dev-local.sh all` must stay green.

**Tech Stack:** Go 1.25, Cobra, `encoding/json`, lib/pq. No new dependencies.

## Global Constraints

- Repo: `seanpham99/dbtools`, branch `main`. **Every change via PR**; `gh pr merge <N> --squash --delete-branch`. No direct pushes.
- **Feature branch `fix/json-contract-and-error-polish` is pre-created and checked out by the parent before dispatch.** Do NOT create branches; do NOT push. Commit locally only. Parent handles push + PR + merge.
- Go toolchain: `export PATH=/tmp/gotool/go/bin:$PATH GOCACHE=/tmp/gocache GOPATH=/tmp/gopath` (see skill).
- Verify before claim: `scripts/dev-local.sh all` (build + lint + unit + smoke) and `gofmt -l .` empty. Integration tests are `//go:build integration` and need live DBs — not required for these changes, but touched engine code must compile.
- `internal/apply.Run` is the ONE apply path. Don't duplicate stepping in commands.
- **Never** put a literal connection string or credential anywhere. Tests use `t.Setenv`.
- Exit-code contract: 0 clean / 1 error / 2 drift-pending. New JSON shapes must still fit it.
- `--json`: stdout machine-only, stderr human. Job-summary record lives in `skills/using-dbtools/private-network-jobs.md`.
- No new dependencies. Keep diffs minimal; one commit per logical change; squash-merge to main.

---

### Task 1: `status`/`plan` — emit `false`/`[]` for state fields (#98)

**Files:**
- Modify: `cmd/status.go:53-62` (statusJSONEntry tags)
- Modify: `cmd/plan.go:50-60` (planJSONEntry tags)
- Test: `cmd/status_test.go` (new), `cmd/plan_test.go:107-162` (extend)

**Interfaces:**
- Consumes: `statusinfo.Status` (already emits all fields), `planJSONEntry` struct in `cmd/plan.go`.
- Produces: unchanged field names; `has_version`, `dirty`, `no_ledger`, `unconfigured`, `pending`, `drift`, `ledger_skipped` always present.

- [ ] **Step 1: Write the failing test**

Add to `cmd/status_test.go`:

```go
func TestStatusJSON_EmitFalseAndEmptyArrays(t *testing.T) {
	entry := statusJSONEntry{
		Target:         "local",
		CurrentVersion: 0,
		HasVersion:     false,
		Dirty:          false,
		Pending:        []string{},
		NoLedger:       true,
		Unconfigured:   false,
	}
	b, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{`"has_version":false`, `"dirty":false`, `"pending":[]`, `"no_ledger":true`, `"unconfigured":false`} {
		if !strings.Contains(got, want) {
			t.Fatalf("marshaled status entry = %s, want it to contain %s", got, want)
		}
	}
}
```

And in `cmd/plan_test.go`, add a companion asserting `pending`, `drift`, `ledger_skipped` always appear as `[]`/`false`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/ -run 'TestStatusJSON_EmitFalseAndEmptyArrays|TestPlanJSON' -v`
Expected: FAIL — `"has_version":false` etc. absent from marshaled output because of `omitempty`.

- [ ] **Step 3: Drop `omitempty` from state fields**

In `cmd/status.go`, change the struct to:

```go
type statusJSONEntry struct {
	Target         string   `json:"target"`
	CurrentVersion uint64   `json:"current_version"`
	HasVersion     bool     `json:"has_version"`
	Dirty          bool     `json:"dirty"`
	Pending        []string `json:"pending"`
	NoLedger       bool     `json:"no_ledger"`
	Unconfigured   bool     `json:"unconfigured"`
	Error          string   `json:"error,omitempty"`
}
```

(Keep `omitempty` on `Error` only — genuinely optional.)

In `cmd/plan.go`, same treatment: drop `omitempty` from `HasVersion`, `Dirty`, `Pending`, `Drift`, `LedgerSkipped`; keep it on `Error`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ -run 'TestStatusJSON_EmitFalseAndEmptyArrays|TestPlanJSON' -v`
Expected: PASS.

- [ ] **Step 5: Full check + commit**

Run: `scripts/dev-local.sh all && gofmt -l .`
Expected: all green, `gofmt -l .` empty.

```bash
git add cmd/status.go cmd/plan.go cmd/status_test.go cmd/plan_test.go
git commit -m "fix(json): emit false and empty arrays for status/plan state fields (#98)"
```

---

### Task 2: `up --json` — single document, folded completion (#99)

**Files:**
- Modify: `cmd/up.go:42-49` (JSON emission)
- Modify: `cmd/jobsummary.go` (signature/behavior)
- Modify: `cmd/push.go`, `cmd/down.go`, `cmd/reset.go`, `cmd/rollback.go`, `cmd/repair.go`, `cmd/adopt.go` (call sites of `emitJobSummary`)
- Test: `cmd/jobsummary_test.go`, `cmd/up_test.go` (new)

**Interfaces:**
- Consumes: existing `emitJobSummary(err *error)` calls; `statusinfo.Status` JSON shape.
- Produces: `up --json` emits exactly ONE JSON document: `{"target":...,"current_version":...,"has_version":...,"dirty":...,"pending":[...],"ok":true}`. `push`, `down`, `reset`, `rollback`, `repair`, `adopt` continue to emit their single state document; no second `job_complete` line on stdout.

- [ ] **Step 1: Write the failing test**

In `cmd/up_test.go`:

```go
func TestUpJSON_SingleDocument(t *testing.T) {
	// Set up a minimal in-memory sqlite target via t.Setenv, run up --json
	// against it, capture stdout.
	// Assert: strings.Count(stdout, "\n") == 1 and json.Unmarshal succeeds.
}
```

Use the existing sqlite test harness pattern from `cmd/plan_test.go` (config + `DBTOOLS_TEST_SQLITE_URL`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run TestUpJSON_SingleDocument -v`
Expected: FAIL — stdout contains two lines.

- [ ] **Step 3: Fold completion into `up`'s status document**

In `cmd/up.go`, replace the JSON block:

```go
if jsonOutput {
	b, err := json.Marshal(struct {
		statusinfo.Status
		OK bool `json:"ok"`
	}{Status: status, OK: true})
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}
```

And in `cmd/jobsummary.go`, change `emitJobSummary` to a no-op when the command already emitted its own document — simplest correct approach: `up` stops calling `emitJobSummary`; keep the deferred summary for `push`/`down`/`reset`/`rollback`/`repair`/`adopt` which still emit a state document then rely on the summary for the completion marker. Update the doc comment accordingly. Ensure `push` — which wraps `applyRun` like `up` — is checked for the same two-doc pattern and folded the same way.

- [ ] **Step 4: Update jobsummary tests**

`cmd/jobsummary_test.go` currently asserts `up --target prod --json` contains `job_complete` (#55 case). Change that expectation: `up` no longer emits a second document; assert the folded `"ok":true` is inside the single object.

- [ ] **Step 5: Full check + commit**

Run: `scripts/dev-local.sh all && gofmt -l .`
Expected: all green.

```bash
git add cmd/up.go cmd/jobsummary.go cmd/push.go cmd/jobsummary_test.go cmd/up_test.go
git commit -m "fix(json): up emits a single JSON document with folded completion (#99)"
```

---

### Task 3: Stated `--json` contract (#100)

**Files:**
- Modify: `docs/exit-codes.md` (add contract section)
- Modify: `skills/using-dbtools/private-network-jobs.md` (align job-summary docs with Task 2)
- Modify: `README.md` (one line pointing at the contract)

**Interfaces:**
- Consumes: the shapes settled in Tasks 1–2.
- Produces: documented, checkable contract — one doc per invocation; every field always present; empty lists `[]` never `null`; `adopt` nil-slice fix.

- [ ] **Step 1: Fix `adopt`'s `null` vs `[]`**

In `internal/adopt/adopt.go`, `BuildPlan` returns slices initialized from `var matched []uint64` etc. — marshal as `null` when empty. Change to `matched := []uint64{}`, `pending := []uint64{}`, `orphan := []uint64{}`. Add a unit test in `internal/adopt/adopt_test.go` asserting an empty plan marshals with `[]` not `null`.

- [ ] **Step 2: Add the contract to docs/exit-codes.md**

Under "## Machine JSON Output (`--json`)", add:

```markdown
### JSON contract

- **One document per invocation.** No command emits a second JSON value on
  stdout. A command that has progress/events to stream emits NDJSON — every
  line a complete JSON object — and says so in its docs. `up`, `status`,
  `adopt` are single-document today.
- **Every documented field is always present.** State fields never use
  `omitempty`: `false` and `[]` are emitted explicitly. Absence means the
  field does not exist in this version of the tool, not "false".
  (Exception: `error` — genuinely optional.)
- **Empty lists are `[]`, never `null`.** A nil slice is a bug.
- **Consumers should validate the shape and fail closed** on any field they
  don't recognise, rather than defaulting silently.
```

- [ ] **Step 3: Update the private-network-jobs doc**

`skills/using-dbtools/private-network-jobs.md` shows `{"event":"job_complete",...}` as a separate line. Update to reflect that `up --json` now folds completion into its object; the summary record remains for the other mutating commands.

- [ ] **Step 4: Commit**

```bash
git add docs/exit-codes.md skills/using-dbtools/private-network-jobs.md README.md internal/adopt/adopt.go internal/adopt/adopt_test.go
git commit -m "docs(json): state the --json contract; adopt emits [] not null (#100)"
```

---

### Task 4: `adopt` — targeted orphan gate `--allow-orphans-before` (#101)

**Files:**
- Modify: `cmd/adopt.go` (flag + gate)
- Modify: `internal/adopt/adopt.go` (helper `OrphansBelow`)
- Test: `cmd/adopt_test.go` (new), `internal/adopt/adopt_test.go`

**Interfaces:**
- Consumes: `adopt.Plan{Matched, Pending, Orphan []uint64}`.
- Produces: `--allow-orphans-before <version>` flag; gate: refuse if any orphan ≥ version, allow if all orphans < version. `--force` unchanged (still all-or-nothing). `adopt --json` already reports `orphan` — wrapper can diff.

- [ ] **Step 1: Write the failing test**

In `cmd/adopt_test.go`:

```go
func TestAdoptAllowOrphansBefore(t *testing.T) {
	// Build a Plan with Orphan = [19990101000000, 19990202000000],
	// Matched = [20260101000000]. 
	// Call the gate helper with version 20260101000000:
	//   - no error when all orphans < version
	//   - error when an orphan >= version
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run TestAdoptAllowOrphansBefore -v`
Expected: FAIL — no such helper yet.

- [ ] **Step 3: Implement the gate**

In `cmd/adopt.go`:

- Add flag `adoptAllowOrphansBefore uint64` (default 0 = disabled): `adoptCmd.Flags().Uint64Var(&adoptAllowOrphansBefore, "allow-orphans-before", 0, "allow orphan history rows below this version (pre-squash history); orphans at or above it are still a hard stop")`
- Add a helper in `internal/adopt`:

```go
// OrphansBelow reports whether every orphan version is strictly below
// the given version — i.e. this is a squashed-history import where the
// orphans are pre-baseline rows and expected.
func OrphansBelow(orphans []uint64, version uint64) bool {
	for _, v := range orphans {
		if v >= version {
			return false
		}
	}
	return true
}
```

- Replace the gate at `cmd/adopt.go:104-109`:

```go
if len(plan.Orphan) > 0 && !adoptForce {
	if adoptAllowOrphansBefore != 0 && adopt.OrphansBelow(plan.Orphan, adoptAllowOrphansBefore) {
		// pre-baseline orphans expected — proceed
	} else {
		return &ExitCodeError{Code: 1, Message: fmt.Sprintf("adopt found %d orphan history row(s) with no matching migration file — if these are pre-squash rows, pass --allow-orphans-before <baseline-version>; otherwise pass --force to proceed anyway", len(plan.Orphan))}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ ./internal/adopt/ -run 'TestAdoptAllowOrphansBefore|TestOrphansBelow' -v`
Expected: PASS.

- [ ] **Step 5: Full check + commit**

Run: `scripts/dev-local.sh all && gofmt -l .`
Expected: all green.

```bash
git add cmd/adopt.go internal/adopt/adopt.go cmd/adopt_test.go internal/adopt/adopt_test.go
git commit -m "feat(adopt): targeted --allow-orphans-before for squashed-history imports (#101)"
```

---

### Task 5: Postgres connection SSL-hint diagnostic (#102)

**Files:**
- Modify: `internal/engine/postgresengine/pgdiagnostic.go` (new function)
- Modify: `internal/engine/postgresengine/pgerror.go` (call it)
- Modify: `internal/engine/postgresengine/postgresengine.go` (Open-time wiring if needed)
- Test: `internal/engine/postgresengine/pgdiagnostic_test.go`

**Interfaces:**
- Consumes: `pq.Error` with `Code == "08001"` or message containing `SSL is not enabled`.
- Produces: `SSLDiagnostic() string` — a hint block appended to the error, mentioning `?sslmode=disable` and that dbtools defaults to requiring SSL unlike some Postgres clients.

- [ ] **Step 1: Write the failing test**

In `pgdiagnostic_test.go`, add a table-driven test that constructs a `*pq.Error` with the SSL message and asserts the diagnostic contains `sslmode=disable`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/postgresengine/ -run TestSSLDiagnostic -v`
Expected: FAIL — function not defined.

- [ ] **Step 3: Implement the diagnostic**

In `pgdiagnostic.go`:

```go
// SSLDiagnostic returns a hint when a connection error is the classic
// lib/pq SSL mismatch: dbtools requires SSL by default (sslmode=require)
// whereas many Postgres clients default to no SSL, so a DATABASE_URL that
// works elsewhere can fail here with "pq: SSL is not enabled on the server".
func SSLDiagnostic(err error) string {
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) || pqErr == nil {
		return ""
	}
	msg := strings.ToLower(pqErr.Message)
	if pqErr.Code == "08001" && strings.Contains(msg, "ssl") || strings.Contains(msg, "ssl is not enabled") {
		return "dbtools connects with sslmode=require by default, unlike some other Postgres clients. If this server genuinely has no TLS (a local container, a CI service container), add ?sslmode=disable to the connection string."
	}
	return ""
}
```

In `pgerror.go`, extend `DiagnosePostgresError` to append the SSL hint when `SSLDiagnostic(err) != ""`. Also check whether `mysqlengine`/`mssqlengine` have a comparable first-connection trip-up worth a one-line hint; if not, note it in the PR body rather than building it.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/engine/postgresengine/ -run TestSSLDiagnostic -v`
Expected: PASS.

- [ ] **Step 5: Full check + commit**

Run: `scripts/dev-local.sh all && gofmt -l .`
Expected: all green.

```bash
git add internal/engine/postgresengine/pgdiagnostic.go internal/engine/postgresengine/pgdiagnostic_test.go internal/engine/postgresengine/pgerror.go
git commit -m "feat(pg): SSL connection-error hint naming sslmode=disable (#102)"
```

---

### Task 6: `up`/`push`/`down` — silence usage on operational refusals (#97.1)

**Files:**
- Modify: `cmd/up.go`, `cmd/push.go`, `cmd/down.go` (RunE preambles)
- Test: extend `cmd/plan_test.go`-style SilenceUsage test; new checks in each command's test

**Interfaces:**
- Consumes: the `ExitCodeError` type + existing pattern from `plan.go:27-35`/`verify.go:20-26`.
- Produces: `SilenceUsage=true` set only when the returned error is an `ExitCodeError` (operational refusal), preserving flag-parse usage for genuine usage errors.

- [ ] **Step 1: Write the failing test**

In each of `cmd/up_test.go`, `cmd/push_test.go`, `cmd/down_test.go`, add:

```go
func TestUpSilenceUsageOnRefusal(t *testing.T) {
	// Trigger an operational refusal (e.g. up --target remote without --yes,
	// or a dirty-ledger refusal). Assert cmd.SilenceUsage is true and the
	// output does not contain "Usage:".
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/ -run 'TestUpSilenceUsageOnRefusal|TestPushSilenceUsageOnRefusal|TestDownSilenceUsageOnRefusal' -v`
Expected: FAIL — usage block printed.

- [ ] **Step 3: Apply the pattern to the three commands**

In each `RunE`, mirror `plan.go`:

```go
RunE: func(cmd *cobra.Command, args []string) error {
	// See plan's identical rationale for this explicit reset.
	cmd.SilenceUsage = false
	err := runX(...)
	var exitErr *ExitCodeError
	if errors.As(err, &exitErr) {
		cmd.SilenceUsage = true
	}
	return err
},
```

Apply the same treatment to `repair` (it shares the dirty-ledger refusal class). `diff` already does this unconditionally (#90) — leave it.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./cmd/ -run 'TestUpSilenceUsageOnRefusal|TestPushSilenceUsageOnRefusal|TestDownSilenceUsageOnRefusal' -v`
Expected: PASS.

- [ ] **Step 5: Full check + commit**

Run: `scripts/dev-local.sh all && gofmt -l .`
Expected: all green.

```bash
git add cmd/up.go cmd/push.go cmd/down.go cmd/repair.go cmd/up_test.go cmd/push_test.go cmd/down_test.go
git commit -m "fix(cmd): silence cobra usage on operational refusals for up/push/down/repair (#97)"
```

---

### Task 7: `repair`/`rollback` — drop the dead "cursor" wording (#97.2)

**Files:**
- Modify: `cmd/repair.go:114-118`, `internal/repair/repair.go:24-30,116-120`
- Modify: `cmd/rollback.go:80-81`, `internal/rollback/rollback.go:17-18`
- Test: update assertions in `cmd/repair_test.go`, `cmd/rollback_test.go` if they reference "cursor"

**Interfaces:**
- Consumes: `Result.NewCursor`/`HasCursor` (internal, not part of the documented JSON contract).
- Produces: user-facing output says "now at version N" / "no applied versions remain" instead of "cursor". Internal field names may stay (avoid churn) but the JSON key `new_cursor`/`has_cursor` should be renamed to `new_version`/`has_version` for consistency with the stated contract (#100).

- [ ] **Step 1: Update output strings**

`cmd/repair.go:118`: `"cursor now at %d"` → `"now at version %d"`.
`cmd/repair.go:115`: `"(cursor untouched)"` → `"(no applied versions remain)"`.
`cmd/rollback.go:81`: `"cursor recomputed to version %d"` → `"now at version %d"`.

- [ ] **Step 2: Rename JSON fields**

`internal/repair/repair.go:24-25`: `NewCursor` → `NewVersion`, `HasCursor` → `HasVersion` (update `json:"new_cursor"` → `json:"new_version"`, `json:"has_cursor"` → `json:"has_version"`). Same in `internal/rollback/rollback.go:17-18`. Update all references (`cmd/repair.go:114-118`, `cmd/rollback.go:80-81`, internal tests).

- [ ] **Step 3: Run tests**

Run: `go test ./cmd/ ./internal/repair/ ./internal/rollback/ -v`
Expected: PASS.

- [ ] **Step 4: Full check + commit**

Run: `scripts/dev-local.sh all && gofmt -l .`
Expected: all green.

```bash
git add cmd/repair.go cmd/rollback.go internal/repair/repair.go internal/rollback/rollback.go
git commit -m "fix(repair,rollback): say version, not the deleted cursor model (#97)"
```

---

### Task 8: Suppress `EnsureSchema` schema-maintenance NOTICEs (#97.3)

**Files:**
- Modify: `internal/engine/postgresengine/ledger.go` (EnsureSchema)
- Modify: `internal/engine/postgresengine/postgresengine.go` (Open — notice handler)
- Test: `internal/engine/postgresengine/ledger_integration_test.go` (gated) + unit where feasible

**Interfaces:**
- Consumes: the notice handler installed in `Open`; `EnsureSchema`'s `ADD COLUMN IF NOT EXISTS` statements.
- Produces: `EnsureSchema` runs with notices suppressed so routine commands don't print `NOTICE: column ... already exists, skipping`; migration `RAISE NOTICE` still reaches the log.

- [ ] **Step 1: Write the test (integration, gated)**

In `ledger_integration_test.go`, add a test that runs `EnsureSchema` twice on a fresh ledger and asserts the second run's captured notice stream contains no `already exists, skipping` NOTICEs.

- [ ] **Step 2: Implement suppression**

The cleanest seam: in the notice handler installed in `Open`, suppress NOTICEs whose message matches `already exists, skipping` (emitted by `ADD COLUMN IF NOT EXISTS`). Keep all other NOTICEs (migration `RAISE NOTICE`). Alternatively, gate a `suppressSchemaNotices` flag on the connection for the duration of `EnsureSchema`. Prefer the message-filter approach — one place, no flag plumbing.

```go
connector := pq.ConnectorWithNoticeHandler(connector, func(n *pq.Error) {
	if strings.Contains(n.Message, "already exists, skipping") {
		return
	}
	logger.Infof("postgres: %s: %s", n.Severity, n.Message)
})
```

- [ ] **Step 3: Run integration test if a Postgres is available; otherwise compile-check**

Run: `go build ./... && go vet ./...`
Expected: green. If `DBTOOLS_TEST_POSTGRES_URL` is set, also run the gated integration test.

- [ ] **Step 4: Full check + commit**

Run: `scripts/dev-local.sh all && gofmt -l .`
Expected: all green.

```bash
git add internal/engine/postgresengine/postgresengine.go internal/engine/postgresengine/ledger_integration_test.go
git commit -m "fix(pg): suppress EnsureSchema 'already exists' NOTICEs on routine commands (#97)"
```

---

### Task 9: Docs sweep + final PR

**Files:**
- Modify: `docs/exit-codes.md` (verify Task 3 additions match final shapes)
- Modify: `README.md` (JSON contract pointer)
- Modify: `docs/roadmap.md` (note the polish if it belongs in a phase row)

**Interfaces:**
- Consumes: all prior tasks.
- Produces: one coherent PR description mapping each issue to its task/commit.

- [ ] **Step 1: Re-verify full suite**

Run: `scripts/dev-local.sh all && gofmt -l .`
Expected: all green.

- [ ] **Step 2: Re-read the JSON examples in docs**

Confirm `status --json`, `up --json`, `adopt --json` examples in `docs/exit-codes.md` match the post-Task-1/2/3 shapes.

- [ ] **Step 3: Open the PR**

```bash
# Branch fix/json-contract-and-error-polish already exists (parent-created) and is checked out.
git push -u origin fix/json-contract-and-error-polish
gh pr create --title "fix: JSON contract, adopt orphans gate, connection/error polish" \
  --body "Closes #97, #98, #99, #100, #101, #102. See individual commits for per-issue detail."
```

- [ ] **Step 4: Verify CI green + merge**

Run: `gh pr checks <N> --watch`
Expected: build/vet/unit + integration (mssql/postgres/sqlite) + GitGuardian all pass. Then:

```bash
gh pr merge <N> --squash --delete-branch
```

---

## Self-Review

- **Spec coverage:** #98 → Task 1; #99 → Task 2; #100 → Task 3; #101 → Task 4; #102 → Task 5; #97.1 → Task 6; #97.2 → Task 7; #97.3 → Task 8; docs/PR → Task 9. All seven issues mapped.
- **Placeholder scan:** The only intentional open item is Task 5's "check MySQL/MSSQL for an equivalent" — scoped as a PR-body note, not a build requirement. All code steps carry full snippets.
- **Type consistency:** `statusJSONEntry`/`planJSONEntry` tags, `adopt.OrphansBelow`, `SSLDiagnostic`, `emitJobSummary` signature are named identically across the tasks that reference them. `internal/repair`/`internal/rollback` field renames in Task 7 are updated at every reference site named.
- **Risks:** Task 2's `emitJobSummary` change touches every mutating command's output shape — the jobsummary tests must be updated in the same task or CI breaks. Task 7's field renames are internal but cross package boundaries (`cmd` + `internal/repair` + `internal/rollback` + tests) — same commit, verified by `go build ./...`.
