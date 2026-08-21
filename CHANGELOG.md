# Changelog

All notable changes to `dbtools` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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
