---
name: using-dbtools
description: Use when creating database migrations, running migration commands, verifying schema ledger drift, resetting local database state, or repairing migration history using dbtools CLI.
---

# Using dbtools

## Overview
`dbtools` is the standalone migration authority and local database dev loop for MSSQL/Postgres projects. It enforces version-sync migration execution using named targets and environment variable connection strings.

## Terminology & Key Concepts

| Term | Definition | Do NOT Say / Use |
|---|---|---|
| **target** | Named DB environment (`local`, `prod`) in `dbtools.toml` mapping to an environment variable (`url_env`). | Hardcoded DB URLs in `dbtools.toml` |
| **push** | Applies pending migrations to a named target (version-sync). | `deploy`, `schema-sync`, `sync` |
| **version-sync** | Guarantees applied migration history matches local `.sql` files up to latest. | "Full schema sync" (dbtools does not do DDL diffing) |
| **reset** | Local-only: drops, recreates DB, replays all migrations, runs `seed.sql`. | `rollback`, `restore` |
| **seed.sql** | Single optional root SQL file run automatically after `reset`. | Multiple seed files or fixture scripts |
| **ledger** | `dbtools_migration_history` table tracking per-migration `applied`/`reverted` state. | Generic "history table" |
| **repair** | Corrects ledger status for target versions. Replaces deprecated `stamp`. | `stamp` (removed in v1) |

## Quick Command Reference

```bash
# Scaffold new project config & directory
dbtools init

# Create new migration file ({timestamp}_{name}.up.sql)
dbtools new <migration_name>

# Apply pending migrations to target (default: local)
dbtools up [--target local]

# Apply pending migrations to named target (explicit target name)
dbtools push <target_name>

# Start/stop tool-owned ephemeral local MSSQL container
dbtools start
dbtools stop

# Local-only reset: drop, recreate, replay migrations, run seed.sql
dbtools reset

# Check status of applied/pending migrations
dbtools status [--json]

# Preview pending migrations + drift (agent/CI-safe: exit 0 = applyable, 2 = pending/drift)
dbtools plan [--target X] [--json]

# Verify migration ledger objects exist in target DB (drift check)
dbtools verify <target_name> [--json]
# Exit codes: 0 clean, 1 error, 2 drift/pending — see docs/exit-codes.md

# Apply .down.sql migrations in reverse (destructive; protected targets need --preview --yes)
dbtools down <target_name> [N] [--preview] [--yes]

# Ledger-only soft-revert (marks 'reverted', never data-destroying) — safe prod verb
dbtools rollback <target_name> [--yes]

# Repair ledger status (replaces old 'stamp' command)
dbtools repair <target_name> <version>:<status> --yes [--force]

# Generate pydantic BaseModel classes from live DB schema
dbtools generate [target] [--out db_models.py] [--yes] [--check]
# --yes required when target is prod. --check exits non-zero if the file on disk
# is stale instead of writing (use in CI to catch un-regenerated db_models.py).

# Generate TypeScript interfaces (+ optional zod schemas) from live DB schema
dbtools generate [target] --lang ts [--zod] [--out db_models.ts]

# Read-only TUI status dashboard ('r' to refresh, 'q' to quit)
dbtools dashboard
```

## Configuration (`dbtools.toml`)

`dbtools.toml` must exist at project root. Connection strings are NEVER placed in `dbtools.toml` — always reference environment variables via `url_env`.

```toml
migrations_dir = "migrations"

[targets.local]
url_env = "DBTOOLS_LOCAL_URL"

[targets.prod]
url_env = "DBTOOLS_PROD_URL"
```

## Workflow & Common Tasks

### 1. Creating a New Database Migration
1. Run `dbtools new <descriptive_name>` to create `migrations/{timestamp}_{name}.up.sql`.
2. Write standard SQL DDL inside generated `.up.sql` file.
3. Test locally by running `dbtools up` or `dbtools reset`.

### 2. Resetting Local Dev Database
1. Run `dbtools start` if container is not running.
2. Ensure `seed.sql` exists at project root if test data is needed.
3. Run `dbtools reset` to drop local DB, replay migrations, and execute `seed.sql`.

### 3. Checking for Drift & Repairing Ledger
1. Run `dbtools verify <target>` to check if ledger objects exist in live DB.
2. If drift or state mismatch is found, fix ledger using `dbtools repair <target> <version>:<status> --yes`.

### 4. Generating Pydantic Models for Downstream Code
1. Ensure migrations are applied (`dbtools up` or `dbtools reset`).
2. Run `dbtools generate local --out dags/db_models.py`.
3. Commit updated `dags/db_models.py` alongside your migration.

## Red Flags - STOP and Correct

- ❌ Hardcoding connection strings or password credentials in `dbtools.toml`.
- ❌ Using non-existent commands: `dbtools migrate`, `dbtools sync`, `dbtools stamp`.
- ❌ Expecting `dbtools push` to diff live schema (it only performs version-sync).
- ❌ Creating multiple seed files instead of single root `seed.sql`.
- ❌ Modifying applied migration files instead of scaffolding a new one with `dbtools new`.
- ❌ Running `dbtools down` on a protected target without `--preview --yes` (refused).
- ❌ Calling `dbtools plan`/`verify` and ignoring the exit code — `2` means drift/pending, not success.

## Rationalizations Table

| Excuse | Reality |
|---|---|
| "I can put localhost URL directly in `dbtools.toml`" | Violates security & convention. Always use `url_env` (e.g. `DBTOOLS_LOCAL_URL`). |
| "I'll use `dbtools stamp` to fix migration state" | `stamp` command is removed. Use `dbtools repair`. |
| "I need to run rollback on production" | Use `dbtools rollback` (ledger-only, non-destructive) or `dbtools down` with `--preview --yes` on a protected target — never silently. |
