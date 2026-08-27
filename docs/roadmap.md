# dbtools roadmap

Status: **living document** — updated as features land. Community input welcome via issues.

## Direction

dbtools is a database migration and ops tool for MSSQL, Postgres, and SQLite, built for
engineering teams and AI coding agents. Today it is a free, self-hosted CLI. The
architecture deliberately keeps the door open to hosted/enterprise offerings later —
every command is CLI-first with structured output, so a future service can wrap the
same core.

## Design constraints

1. **CLI-first, structured output everywhere.** Every command emits `--json`;
   a stable exit-code contract distinguishes clean / drift / error. Machine output on
   stdout only; human logs on stderr.
2. **Read-only by default.** `verify`, `plan`, `status`, `doctor` never write.
   Destructive operations require explicit flags and human-in-the-loop confirmation on
   production targets.
3. **The ledger is truth.** Every applied migration records its status, content hash,
   and timestamp. No command writes the ledger without recording the file it executed.

## Phases

| Phase | Feature | Notes |
|-------|---------|-------|
| v0.1.2 ✅ | `plan` preview | Read-only preview of pending migrations + ledger drift. Agent/CI-friendly: exit 0 = safe to apply. |
| v0.2 ✅ | **Rollback & down migrations** | `down` (reverse `.down.sql` apply) + ledger-only `rollback`, with production gates. Shipped in v0.2.0. |
| v0.2 ✅ | **Agent ergonomics** | Universal `--json`, `--dry-run`, non-interactive mode, dirty-ledger refusal, exit-code contract. Shipped in v0.2.0. |
| v0.2 ✅ | **TypeScript generation** | `generate --lang ts [--zod]` — Supabase-style interfaces + zod. Shipped in v0.2.0. |
| v0.2 ✅ | **npx installer** | `dbtools-cli` npm wrapper — thin downloader of the GoReleaser binary (5 platforms). Published via OIDC trusted publishing (no token). Shipped 0.2.1. |
| v0.3 ✅ | **`doctor`** | Read-only health/security check: connectivity, version sync, ledger integrity, drift summary, dirty-ledger, basic security flags. Exit 0 clean / 1 error / 2 issues. |
| v0.3 ✅ | **Real-data integration suite** | Committed classicmodels fixture corpus across sqlite/postgres/mssql, consolidated runner in `internal/testutil`, edge-case matrix, golden typegen. |
| v0.3/4 ✅ | **Clone prod→dev** | Shipped: `dbtools clone <source> <dest>`, `internal/clone`. Masking on by default (built-in sensitive-column list + `[clone.mask]` overrides); `--no-mask` is the explicit opt-out. Same-engine only; row-count/WHERE subsetting via `--limit`/`--where`. |
| v0.3/4 ✅ | **MySQL engine** | Shipped: `internal/engine/mysqlengine`, mirroring the mssql/postgres/sqlite seam. classicmodels fixtures ported to MySQL with real FKs. Scope: TABLE/VIEW DDL detection only (no stored procedures — see the implementation plan for why). `internal/container` local-dev support shipped separately (see Docker project lifecycle, below). |
| v0.4 ✅ | **Docker project lifecycle** | Shipped: project-scoped local container/volume naming (path-hash or `[project] name`), dynamic or pinned (`[container] port`) host ports, data persists across `stop`/`start` via a named volume (`stop --no-backup` opts back into a full wipe), new `restart`/`logs` commands, and the missing MySQL container spec. `dbtools reset` on MySQL remains unsupported (separate, deferred gap — `cmd/reset.go` has no `mysql` case yet). |
| v0.5 ✅ | **Private-network job execution** (issue #60) | Shipped: official multi-arch Docker image (`ghcr.io/seanpham99/dbtools`), Postgres diagnostics (NOTICE forwarding, character-offset error translation, SQLSTATE 42501 permission diagnostic), `--log-format=json` structured logging with clean stdout/stderr separation, job-completion summary record, and container-orchestrator retry-semantics docs. Landed as `feat:` commits, correctly bumping past v0.4 per semver — this table is the roadmap's record of *why*, not a manual override of the version number. |
| v0.6 ✅ | **Incumbent-ledger adoption** (issues #61–#63) | Shipped: `dbtools adopt` (import an existing migration ledger + configurable table name/filename convention), ledger-free read-only mode for `plan`/`verify`/`doctor`/`status`, `dbtools diff` (replay-and-compare drift check), `dbtools squash` (verified baseline collapse, safe on fresh and existing databases). |
| v0.7 | **Backup** | Table-stakes backup/restore. |
| Mongo | **C → B → A** | Starts only after SQL is stable (gate = v0.2 shipped — met). See design below. |
| launch | — | Public launch (announcements, directories) happens only after the v0.2/v0.3 features above. |

## Rollback & down migrations

- Support `.down.sql` files (golang-migrate already parses them; dbtools currently
  ignores them).
- `dbtools down <target> [N]` — applies down files in reverse, each recorded in the
  ledger with its content hash.
- `dbtools rollback <target>` — **ledger-level only** soft-revert (marks versions
  reverted). Never data-destroying; the safe verb for production.
- Production gate: `down` requires `--preview --yes` and prints exactly what will be
  destroyed. Local targets unrestricted.
- Rationale: a down migration that drops a column destroys data irrecoverably; several
  production tools ban automatic down-migrations for exactly this reason. The two-verb
  split — destructive `down` vs ledger-only `rollback` — is the answer.

## Agent ergonomics

Adopted patterns (each proven by an existing tool):

1. **Exit-code contract distinguishes drift from error** — Terraform `-detailed-exitcode`
   style: `0` clean, `1` error, `2` drift/changes. Agents branch on it.
2. **`--dry-run` as the pre-apply gate** — `up --dry-run` prints the SQL and exits 0;
   real apply requires `--yes`. Never prompt in non-TTY.
3. **Structured JSON everywhere** — one `--json` flag on all commands, stable field
   names, machine output on stdout only.
4. **Non-interactive mode** — `DBTOOLS_NO_PROMPT=1` (or non-TTY): never block on stdin;
   destructive ops fail closed.
5. **Dirty-ledger refusal** — `up` auto-refuses when the ledger is dirty; verify
   surfaces content-hash drift as an exit-2-class state, not silent success.
6. **Agent skill surface** — ship a `SKILL.md` + `llms.txt` command inventory so agents
   self-restrict to safe commands (pattern: Atlas Agent Skills).

## Clone (prod→dev)

Shipped. See `internal/clone` and the "Clone (prod → dev)" section of
README.md for usage.

## License

MIT. A future hosted/enterprise pivot may revisit licensing (a community-trust event,
not a silent change); today the permissive license maximizes adoption.

## npx installer

- Package: `dbtools-cli` (unscoped; the bare `dbtools` npm name is owned by an
  unrelated project, and scoped names need an npm org).
- Shape: thin npm wrapper — `npx dbtools-cli` downloads the GoReleaser binary for
  the caller's platform (`darwin`/`linux`/`windows` × `amd64`/`arm64`) from GitHub
  Releases, then execs it. **No Go→JS rewrite.**
- Versioning: npm version tracks the Go release version, published by the same
  release pipeline via **OIDC trusted publishing** (GitHub `npm` deployment
  environment + `id-token: write`; no npm token secret).

## Mongo support (post-SQL-stable)

Starts only after the SQL story is stable (gate = v0.2 shipped + npx installer — **met as of 0.2.1**).
The engine abstraction is SQL-shaped today (`Engine.Open` returns `*sql.DB`,
`DDLDialect` parses SQL, `verify` checks SQL object existence); Mongo needs a
non-SQL seam, so it is a semantic fork, not a new engine. Sequence:

1. **C — local dev instance** (start/stop a MongoDB container for local work).
   Cheapest; teaches the non-SQL seam.
2. **B — introspection** (read Mongo collection shapes → emit pydantic/TS models
   via `generate`). Fits the existing `Introspect` seam.
3. **A — migration target** (apply migration scripts to Mongo; a
   `mongo_migrations` collection is the ledger). Hardest; needs the engine-seam
   refactor. Verification is weaker than SQL: no DDL objects to existence-check,
   only collection-name checks + document sampling. Modeled on migrate-mongo /
   mongobee, not on the SQL object-existence model.

## Open questions (deferred)

- Backup scope (v0.7): full vs incremental, engine support, restore test strategy.
- `doctor` exact check list — spec at v0.3 start.
- Exact exit-code values — written up in `docs/exit-codes.md` with v0.2.
- Mongo `generate` output shape — decide at step B (pydantic vs TS).
