# Changelog

All notable changes to `dbtools` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [0.5.0](https://github.com/seanpham99/dbtools/compare/v0.4.0...v0.5.0) (2026-08-26)


### Features

* add log-format flag for structured json logging ([#68](https://github.com/seanpham99/dbtools/issues/68)) ([ef3d832](https://github.com/seanpham99/dbtools/commit/ef3d8320c274a13f547b96d63fdd02c8e179df3c))
* add official multi-arch docker image ([#65](https://github.com/seanpham99/dbtools/issues/65)) ([1e36e55](https://github.com/seanpham99/dbtools/commit/1e36e551eda57a446c2a19d8b5314b43819609e9))
* add postgres error diagnostics and notice forwarding ([#67](https://github.com/seanpham99/dbtools/issues/67)) ([a6f8404](https://github.com/seanpham99/dbtools/commit/a6f840463741b8d761ba1d50129e8df2663cd472))
* implement dbtools adopt command (closes [#61](https://github.com/seanpham99/dbtools/issues/61)) ([#72](https://github.com/seanpham99/dbtools/issues/72)) ([bb62c39](https://github.com/seanpham99/dbtools/commit/bb62c3998a1e18ff7b8a645cc9121c2d78d1d64b))


### Bug Fixes

* address issue [#60](https://github.com/seanpham99/dbtools/issues/60) code-review findings ([#70](https://github.com/seanpham99/dbtools/issues/70)) ([853cfb8](https://github.com/seanpham99/dbtools/commit/853cfb826be28bf42eb4d5249650196af4e01484))

## [0.4.0](https://github.com/seanpham99/dbtools/compare/v0.3.0...v0.4.0) (2026-08-25)


### Features

* **docs:** polish landing page with motion system, native db logos, and centered hero ([#46](https://github.com/seanpham99/dbtools/issues/46)) ([0a88d3a](https://github.com/seanpham99/dbtools/commit/0a88d3a576e5d5d22f1b6fb331390e3ca3621a68))
* **mysql:** add MySQL engine support and `dbtools clone` (prod→dev, masked-by-default) command ([#47](https://github.com/seanpham99/dbtools/pull/47)) ([2ad3e08](https://github.com/seanpham99/dbtools/commit/2ad3e08f7af2d523d47f91eff067e9527c1d121b))
* **container:** project-scoped local Docker container/volume naming, dynamic or pinned host ports, data persistence across `stop`/`start`, new `restart`/`logs` commands, and the missing MySQL container spec ([#50](https://github.com/seanpham99/dbtools/pull/50)) ([308be64](https://github.com/seanpham99/dbtools/commit/308be64c103ed7339f05a40f5b9b5a498d1f3eee))

### Bug Fixes

* **reset:** resolve the maintenance connection from the local target's actual URL instead of a fixed port, and refuse to build one for a non-loopback host ([#50](https://github.com/seanpham99/dbtools/pull/50)) ([589fd92](https://github.com/seanpham99/dbtools/commit/589fd923d1a1a5a17c49abce0f71ba47c336ccc0))

## [0.3.0] - 2026-08-24

v0.3 — health & security doctor audit, classicmodels real-data integration corpus, dirty recovery, and dev-loop enhancements.

### Added

- **Health & Security Audit (`dbtools doctor`)**:
  - `dbtools doctor [target] [--json]` — strictly read-only check across connectivity, ledger integrity (hash comparison), version sync, live object drift summary, dirty ledger detection, and security configuration (target protection and url_env usage).
  - Exit code contract: `0` healthy, `1` error/unreachable, `2` issues detected (drift, dirty, pending migrations, security warnings). Documented in `docs/doctor-checks.md`.
- **Classicmodels Real-Data Integration Suite**:
  - Committed real-data fixture corpus ported across SQLite, PostgreSQL, and MSSQL with canonical schema, foreign keys, and seed data.
  - Consolidated test runner in `internal/testutil` exercising multi-version migration sequences, content hash drift, and golden typegen regression checks.
- **Dirty Migration Recovery (`dbtools force`)**:
  - `dbtools force <version> [--target <target>] [--yes]` — recovers from interrupted or dirty migration states by aligning the tracking version and clearing the dirty bit.
- **Local Dev-Loop DX Enhancements**:
  - `dbtools start` container readiness polling — polls port and ping until database engine accepts connections (`--timeout`, `--no-wait`).
  - Automatic target database creation — creates missing local/unprotected databases on container instances seamlessly during dev loop.
  - `dbtools reset [target]` — allows specifying any unprotected target (defaults to `local`).
  - `dbtools status [target]` — supports positional target filtering and displays `[unconfigured]` for unset environment variables without erroring during general status.

---

## [0.2.0] - 2026-08-21

v0.2 — rollback + agent ergonomics + TypeScript generation.

### Added

- **Down migrations & ledger-only rollback**:
  - `dbtools down <target> [N]` — applies `.down.sql` files in reverse, recorded in the ledger.
  - `dbtools rollback <target>` — ledger-only soft-revert (never data-destroying), the safe prod verb.
  - Production gate: `down` requires `--preview --yes` on protected targets.
- **Agent ergonomics (AI-agent + CI contract)**:
  - Terraform-style exit-code contract: `0` clean, `1` error, `2` drift/pending. Documented in `docs/exit-codes.md`.
  - `--dry-run` for `up`/`push` (prints SQL, applies nothing).
  - Universal `--json` output on all commands.
  - `DBTOOLS_NO_PROMPT=1` / non-TTY fail-closed mode.
  - Dirty-ledger refusal: `up` refuses when the ledger is dirty.
- **TypeScript generation**:
  - `dbtools generate --lang ts [--zod]` — Supabase-style `export interface` per table, optional zod schemas.
  - SQL→TS mapping (int8→bigint, timestamps→string, json→any), reserved-word escaping, `--check` support.
- **npx installer**:
  - `@dbtools/cli` npm wrapper — thin downloader of the GoReleaser binary; version-synced release pipeline.

### Changed

- Engine seams deepened (`internal/migrator.Dir` unified, render dissolved, apply stepping hardened).

---

## [0.1.0] - 2026-08-21

Initial open-source release of `dbtools` — the database migration authority and local dev-loop for modern engineering teams and AI agents.

### Added

- **Multi-Engine Migration Engine**:
  - Native engine support for MSSQL Server, PostgreSQL, and SQLite.
  - PostgreSQL engine with clean session state reset per migration step to ensure migration isolation.
  - MSSQL batch execution supporting `GO` statement splitting and transaction handling.
  - File-based SQLite engine for local dev and lightweight serverless deployments.
- **Migration Ledger & Drift Detection**:
  - `dbtools_migration_history` ledger table tracking migration state (`applied`/`reverted`) and cryptographic SHA-256 content hashes.
  - Read-only `dbtools verify` command checking live database objects (tables, views) against migration definitions and detecting post-apply file mutations.
  - Drop-lifecycle reasoning in `verify` so tables legitimately dropped by subsequent migrations are not falsely flagged as drift.
  - `dbtools repair` command to safely adjust ledger state and synchronize version cursors.
- **Target Safety & Environment Scoping**:
  - Declarative target configurations in `dbtools.toml` with `url_env` mappings to prevent hardcoded credentials.
  - Target protection flags (`protected = true`) ensuring destructive commands (`up`, `reset`) cannot run accidentally against production targets.
  - Explicit confirmation and preview requirements for remote target pushes (`dbtools push`).
- **Python Type Generation**:
  - `dbtools generate` introspects live database schemas and emits type-safe, versioned Pydantic v2 `BaseModel` classes with PascalCased naming.
  - `--check` flag to verify Python model sync against live schemas in CI pipelines.
- **Developer Experience & Observability**:
  - Bubble Tea interactive terminal dashboard (`dbtools dashboard`).
  - Ephemeral MSSQL container dev-loop orchestration (`start`, `stop`, `reset`, and `seed.sql` automation).
  - Standardized Go module path `github.com/seanpham99/dbtools` with one-line installation (`go install`).
