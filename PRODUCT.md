# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

- **Backend & Data Engineers**: Developing services, ETL pipelines, and local dev-loops requiring fast, deterministic migrations across MSSQL, PostgreSQL, and SQLite without heavy runtime dependencies.
- **DevOps & Platform Engineers**: Managing CI/CD pipelines, production release gates, and schema drift audits with zero-drift cryptographic guarantees.
- **Autonomous AI Coding Agents**: Invoking schema migration, planning, drift verification, and code generation non-interactively via strict machine-readable contracts (JSON, Terraform-style exit codes, fail-closed `DBTOOLS_NO_PROMPT=1`).

## Product Purpose

`dbtools` is a lightweight, reliable database migration authority and local dev-loop tool. It guarantees version-synchronized migration execution, immutable ledger tracking (`content_sha256`), read-only schema drift verification, and live schema introspection to typed Pydantic Python or TypeScript/Zod models.

Success means zero accidental schema drift in production, immediate local dev feedback loops, and deterministic, prompt-free execution in automated CI and agent workflows.

## Positioning

Unlike conventional ORM/migration tools that only track a scalar version number, `dbtools` maintains an immutable cryptographic ledger (`content_sha256`) of applied migration files, provides read-only live object drift verification, and provides an agent-first ergonomics contract (predictable exit codes, universal JSON, dry-runs, and protected target safeguards).

## Operating Context

- Local development terminals, Dockerized dev databases (`dbtools start`/`stop`), and interactive TUI dashboards (`dbtools dashboard`).
- CI/CD release workflows (GitHub Actions, GitLab CI) running automated drift checks (`dbtools verify`, `dbtools doctor`, `dbtools generate --check`).
- Autonomous coding agent execution loops running structured plan-and-apply sequences.
- Configuration managed via `dbtools.toml` with target database connection strings injected through environment variables (`url_env`) to prevent secret leakage.

## Capabilities and Constraints

- **Multi-Engine Support**: Native migration engines for MSSQL, PostgreSQL (with session reset isolation), and SQLite.
- **Zero-Drift Cryptographic Ledger**: Migration history table (`dbtools_migration_history`) tracking version timestamps, applied/reverted status, and SHA-256 file content digests.
- **Drift & Integrity Audit**: `verify` and `doctor` inspect content tampering and missing database objects non-destructively.
- **Safe Prod Operations**: Soft ledger revert (`rollback`) vs destructive reverse SQL (`down`), requiring explicit `--preview --yes` on protected targets.
- **Type Generation**: `generate` outputs Pydantic v2 models or TypeScript interfaces (+ Zod schemas) directly from live schema introspection.
- **Zero External Dependencies**: Single self-contained Go binary with npm wrapper (`dbtools-cli`) for JS/TS ecosystems.

## Brand Commitments

- **Name**: `dbtools` (CLI/Go package), `dbtools-cli` (npm distribution).
- **Tone & Personality**: Pragmatic, surgical, developer-centric, rock-solid reliability, fail-closed safety.

## Evidence on Hand

- Complete Go implementation in repository (`cmd/`, `internal/engine/`, `internal/migrator/`, `internal/statusinfo/`).
- Full test matrix & documentation (`README.md`, `docs/exit-codes.md`, `docs/roadmap.md`, `CONTRIBUTING.md`).
- Multi-engine CI workflows in `.github/workflows/`.

## Product Principles

1. **Safety Over Convenience**: Refuse destructive actions on protected targets without explicit multi-step flags (`--preview --yes`); fail-closed by default.
2. **Immutable Ledger Truth**: Cryptographically verify that migration files have not drifted from what was executed.
3. **Agent & Automation Native**: Treat machines and AI agents as first-class CLI citizens with strict exit-code contracts, zero mandatory prompts, and rich structured JSON.
4. **Zero Bloat & Fast Startup**: Minimal runtime overhead, fast execution, and zero external runtime dependencies.

## Accessibility & Inclusion

- CLI/TUI outputs adhere to clear textual formatting, standard color-contrast practices, plain `--json` output options, and graceful degradation in non-TTY environments.
