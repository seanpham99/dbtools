# Handoff: MySQL engine → Clone (prod→dev)

**For:** Antigravity picking up where Claude Code left off.
**Repo:** `github.com/seanpham99/dbtools` (Go 1.25 CLI — database migration authority for MSSQL/Postgres/SQLite, adding MySQL + a new `clone` command).
**Branch:** `main`, tracking `origin/main`, nothing committed yet this session.

## Do this, in order

1. Execute `docs/superpowers/plans/2026-08-25-mysql-engine.md` — 9 tasks, checkbox-tracked, full TDD steps (write test → run red → implement → run green → commit) per task. Task order matters — later tasks depend on earlier ones compiling.
2. Then execute `docs/superpowers/plans/2026-08-25-clone-prod-to-dev.md` — 5 tasks, same format. Independent of MySQL (doesn't touch engine internals), just sequenced second per prior agreement.

Both plans are self-contained: real file paths, real Go code, real SQL, no placeholders. Follow each task's steps literally, run the exact commands given, commit at each task's end with the given message. Each plan's final "Self-review notes" section lists the scope decisions already locked in — don't re-litigate them mid-execution (e.g. MySQL DDL scope is TABLE/VIEW only, no stored procs; Clone requires same-engine source/dest and always refuses a protected dest).

## Repo state right now

- Working tree has **staged, uncommitted** harness files from this session: `AGENTS.md` (root map — read this first, it's the actual map of the codebase), `docs/index.md`, `scripts/dev-local.sh` (one-command build/lint/test/smoke — verified working), `.codegraph/.gitignore`.
- `docs/superpowers/plans/` is new and untracked — the two plan files plus this handoff doc.
- No code changes yet for either plan — both are 100% unimplemented, planning-only.

## Verify-as-you-go

```bash
scripts/dev-local.sh all     # build + gofmt/vet/golangci-lint + unit tests + real CLI smoke test (sqlite, no Docker)
go test ./...                # unit tests only
go test -tags=integration ./internal/engine/mysqlengine/... -v   # needs a live MySQL (see the mysql plan's Task 8 for a throwaway docker run one-liner)
```

## Things Antigravity should know that aren't in the plans

- This is a CLI, not a service — no browser/server to drive. Verification is `go test` + the black-box smoke script.
- Every engine (`internal/engine/{mssqlengine,postgresengine,sqliteengine}`) follows the same 5-method `engine.Engine` interface (`internal/engine/engine.go`) — mysqlengine mirrors that shape exactly, file-for-file against `mssqlengine`'s layout (closest analog: also needs real DSN translation).
- Golden test fixtures (`internal/testutil/testdata/golden/*/models.py`, `models.ts`) are regenerated via `DBTOOLS_TEST_UPDATE=1 go test -tags=integration ...` — an existing repo convention (`internal/testutil/runner.go`), not something to hand-write.
- Safety-gate conventions (`--yes`, `requireUnprotected`, protected-target banners) live in `cmd/openTarget.go` — read it before touching `cmd/clone.go`.

## If something in a plan turns out wrong

Both plans made real engineering calls under uncertainty (e.g. the golang-migrate mysql driver's DSN handling was confirmed by reading its vendored source, not guessed). If live testing contradicts something in a plan, fix forward and note the discrepancy in the PR description — don't silently diverge without a trace.
