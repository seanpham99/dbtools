# AGENTS.md — map, not manual

`dbtools` is a Go CLI: a database migration authority + local dev-loop for
MSSQL, PostgreSQL, and SQLite. It is a CLI, not a service — there is no
server process or browser UI to drive. This file is a table of contents;
depth lives in `docs/` and the root `*.md` files linked below.

## One-command verify

```bash
scripts/dev-local.sh all     # build + lint (gofmt/vet/golangci-lint) + unit tests + CLI smoke test
scripts/dev-local.sh smoke   # black-box: init/new/up/status/verify/lint/reset against a real sqlite db
go test -tags=integration ./...   # needs live MSSQL/Postgres — see CONTRIBUTING.md
```

Run `scripts/dev-local.sh all` before claiming any change done. It needs no
Docker/DB — SQLite is file-based and serverless.

## Golden rules

1. **Never put a literal connection string or credential anywhere** — not in
   `dbtools.toml`, not in code, not in test fixtures. Targets resolve a URL
   from an env var named by `url_env` (`internal/config`). Tests use
   `t.Setenv`.
2. **One apply path.** `internal/apply.Run` is the only code that steps
   migrations and writes the ledger. `up`/`push`/`reset` all call it — don't
   duplicate the stepping loop in a command.
3. **`protected` targets refuse writes.** Any command that mutates a target
   (`up`, `push`, `reset`, `repair`, `down`, `rollback`) must route through
   `requireUnprotected` / the target's `--yes` gate (`cmd/openTarget.go`).
   Read-only commands (`status`, `verify`, `plan`, `doctor`) never gate.
   Non-trivial handling here — read `cmd/openTarget.go` before touching a
   mutating command.
4. **Exit-code contract is load-bearing.** `0` clean, `1` error, `2`
   drift/pending — see `docs/exit-codes.md`. Every new command's exit path
   must fit this, including under `--json`.
5. **Engines are symmetric.** A change to one of `mssqlengine`,
   `postgresengine`, `sqliteengine` (DDL regex, ledger SQL, introspection)
   almost always needs the matching change in the other two — check
   `internal/engine.Engine`'s method set and grep the other two packages.
6. **Dirty ledger cursor blocks everything until `repair`.** Don't add a
   code path that silently proceeds past `dirty=true`.

## Where to look

| Question | Look here |
|---|---|
| Command wiring, flags, exit codes | `cmd/*.go` (one file per command; `cmd/openTarget.go` is the shared preamble) |
| What `dbtools.toml` looks like, target resolution | `internal/config` |
| Engine plugin interface (Open/DDL/Ledger/Introspect) | `internal/engine/engine.go` |
| MSSQL/Postgres/SQLite dialect specifics | `internal/engine/{mssqlengine,postgresengine,sqliteengine}` |
| golang-migrate wrapping, GO-batch splitting, pg session reset | `internal/migrator` |
| The applied/reverted ledger + content-hash drift model | `internal/ledger`, ADR in `docs/adr/002-*.md` |
| Drift detection logic (`verify`) | `internal/verify/collect.go` |
| Local docker containers for dev (`start`/`stop`) | `internal/container` |
| Pydantic / TS+zod codegen | `internal/generate` |
| Terminology (target, push, version-sync, ledger, repair, ...) | `CONTEXT.md` |
| Product positioning, principles | `PRODUCT.md` |
| Visual/design system (docs site only, not the CLI) | `DESIGN.md` |
| Architectural decisions | `docs/adr/` |
| Exit codes / agent & CI contract | `docs/exit-codes.md` |
| `doctor` check reference | `docs/doctor-checks.md` |
| Roadmap / what's not built yet | `docs/roadmap.md` |
| Full command reference, install, examples | `README.md` |
| Dev workflow, test commands, PR process | `CONTRIBUTING.md` |
| Agent-facing command skill (loaded by Claude Code) | `skills/using-dbtools/SKILL.md` |
| npm wrapper (`dbtools-cli`) | `npm/` |

## Project tree (top level)

```
cmd/                   Cobra commands (one file per command + shared helpers)
internal/
  apply/               the one migration-apply path
  config/               dbtools.toml parsing, URL resolution
  container/            docker lifecycle for local MSSQL/Postgres
  dashboard/             bubbletea TUI for `dashboard`
  ddlcheck/              shared ObjectRef type for drift detection
  down/ rollback/ repair/  the other ledger-mutating verbs
  engine/                Engine interface + mssqlengine/postgresengine/sqliteengine
  generate/              pydantic/TS codegen
  ledger/                ledger types shared by all engines
  localenv/              .dbtools/local.env read/write (container-provisioned URL)
  migrator/              golang-migrate wrapper, drivers, Dir (migration file index), Lint
  scaffold/ seed/         `new` filename generation, seed.sql runner
  statusinfo/ verify/     status collection, drift detection
  testdb/ testutil/       integration test helpers + classicmodels fixture corpus
docs/                  architecture, ADRs, exit codes, doctor checks, roadmap
skills/using-dbtools/  agent-facing command skill
npm/                   dbtools-cli npm wrapper (downloads the Go binary)
scripts/dev-local.sh   one-command build/lint/test/smoke
```

## Non-goals (don't build these)

- Full live-schema diffing / column-level sync — `verify` only checks named
  object existence + content hash, deliberately (see `docs/adr/002-*.md`).
- A `stamp` command — removed, replaced by `repair`.
- Domain/business logic of any kind — this tool carries none.
