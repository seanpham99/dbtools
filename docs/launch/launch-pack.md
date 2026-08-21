# dbtools — Open Source Launch Pack

> [!CAUTION]
> **DRAFTS ONLY**: Do not submit, post, or publish any of these materials without explicit maintainer approval and coordinated timing.

---

## 1. DevHunt Submission

- **Product Name**: `dbtools`
- **Tagline**: Multi-engine database migration authority & local dev-loop in Go
- **Category**: Developer Tools / Database
- **Repository URL**: `https://github.com/seanpham99/dbtools`
- **Homepage URL**: `https://seanpham99.github.io/dbtools/`
- **Short Pitch (under 250 chars)**:
  A zero-dependency Go CLI for MSSQL, PostgreSQL, and SQLite with cryptographic ledger tracking, read-only schema drift verification, and live Pydantic model generation.
- **Description**:
  dbtools is a standalone database migration tool and local development loop designed for modern engineering teams and autonomous AI coding agents.

  Traditional migration runners only track a single version number, leaving systems vulnerable to post-apply file tampering and out-of-band table drops. dbtools introduces:
  - **Cryptographic Ledger**: SHA-256 content hashes in `dbtools_migration_history` guarantee files haven't mutated since execution.
  - **Drop-Aware Drift Verification**: `dbtools verify` inspects live catalog objects while accounting for subsequent drop migrations.
  - **Target Protection**: Declares named environments via environment variables (`url_env`), refusing destructive commands on production targets.
  - **Pydantic Type Generation**: `dbtools generate` renders live schema metadata directly into typed Python `pydantic.BaseModel` contracts with `--check` CI validation.
  - **Interactive TUI Dashboard**: Real-time Bubble Tea terminal dashboard for local status monitoring.

---

## 2. Hacker News ("Show HN") Post Draft

**Title**: `Show HN: dbtools – Migration authority with ledger drift detection and Pydantic export (Go)`

**Text**:

Hi HN! I built `dbtools` (https://github.com/seanpham99/dbtools), an open-source Go CLI that acts as a migration authority and local dev-loop for MSSQL, PostgreSQL, and SQLite.

### Why build another migration tool?
While maintaining multi-database backend services, we repeatedly encountered three major problems with traditional runners:
1. **Silent file mutations**: Once a migration runs, nothing prevents someone from inadvertently editing the file in git later, leaving production in an unknown state.
2. **Untracked schema drift**: Tables dropped or altered out-of-band don't affect the single version cursor in `schema_migrations`, leading to confusing deployment failures.
3. **Secret exposure & target confusion**: Connection strings frequently leak in configuration files or get accidentally targeted with destructive commands.

### What dbtools does differently:
- **Immutable Ledger**: Every migration records its applied status alongside a SHA-256 hash of the exact file content executed.
- **Read-Only Drift Verification (`dbtools verify`)**: Checks that live database objects exist and hashes match. It understands lifecycle drops so tables legitimately dropped by later migrations aren't flagged as drift.
- **Protected Targets**: Defined in `dbtools.toml` via env var bindings (`url_env`), with safety guards blocking accidental `up` or `reset` on production environments.
- **Python Type Generation (`dbtools generate`)**: Introspects live database schemas into clean Pydantic v2 BaseModels for Python services and ETL workers.
- **Zero Runtime Dependencies**: Written in pure Go with native drivers and Bubble Tea TUI.

Installation:
```bash
go install github.com/seanpham99/dbtools@latest
```

Docs & Architecture ADRs: https://seanpham99.github.io/dbtools/

I'd love feedback on the ledger verification model and multi-engine dialect handling!

---

## 3. Reddit (r/golang) Post Draft

**Title**: `dbtools: A Go CLI for multi-engine database migrations with ledger drift verification and Pydantic generation`

**Post Body**:

Hey r/golang!

I'd like to share `dbtools` (https://github.com/seanpham99/dbtools), a Go CLI tool for managing schema migrations across SQL Server (MSSQL), PostgreSQL, and SQLite.

### Key Highlights:
- **Ledger Verification**: Tracks applied migrations with SHA-256 hashes in `dbtools_migration_history`. `dbtools verify` checks live schema objects and alerts on file drift.
- **Session-Clean PostgreSQL Driver**: Isolates migration steps with session resets to avoid state contamination across migrations.
- **Target Protection**: Separates configuration from credentials using environment variable references (`url_env`). Destructive operations require explicit confirmation.
- **Pydantic Model Generation**: `dbtools generate` introspects live schema tables into typed Python models with a `--check` flag for CI drift gates.
- **TUI Dashboard**: Built with `charmbracelet/bubbletea`.

Check out the repo at https://github.com/seanpham99/dbtools. Feedback and contributions are welcome!

---

## 4. Awesome Lists Inclusion Drafts

### `awesome-go` (Category: Database / Database Schema Migration)

```markdown
- [dbtools](https://github.com/seanpham99/dbtools) - Multi-engine database migration authority and local dev-loop for MSSQL, PostgreSQL, and SQLite with cryptographic ledger drift verification.
```

### `awesome-database-tools` (Category: Migration Tools)

```markdown
- [dbtools](https://github.com/seanpham99/dbtools) - Lightweight CLI in Go supporting MSSQL, PostgreSQL, and SQLite with immutable ledger tracking, schema drift detection, and Pydantic model generation.
```

---

## 5. Case Study: The 5-PR Campaign & OSS Readiness

### Background & Architecture Sprint:
- **PR #2**: Built CI matrix across live MSSQL 2022, PostgreSQL 17, and SQLite services. Resolved MSSQL batch cursor advance bug with Go driver wrapper.
- **PR #3**: Introduced cryptographic SHA-256 content hashing to `dbtools_migration_history`. Made `verify` read-only and prevented `sync` on dirty cursors.
- **PR #4**: Enforced target environment protection flags (`protected = true`) and scoped connection URL handlers.
- **PR #5**: Implemented PostgreSQL per-step session resets to guarantee migration isolation.
- **PR #6**: Refined Pydantic model generator with identifier sanitization and syntax safety.
- **OSS Sprint (PRs #7-#10)**: Canonical Go module rename (`github.com/seanpham99/dbtools`), binary purging, README overhaul, community standards, ADR documentation, and static GitHub Pages launch.
