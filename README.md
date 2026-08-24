# dbtools

[![CI](https://github.com/seanpham99/dbtools/actions/workflows/ci.yml/badge.svg)](https://github.com/seanpham99/dbtools/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/seanpham99/dbtools.svg)](https://pkg.go.dev/github.com/seanpham99/dbtools)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**dbtools** is a lightweight, reliable database migration authority and local dev-loop tool for **MSSQL**, **PostgreSQL**, and **SQLite**. Built in Go for high performance and zero external runtime dependencies, `dbtools` guarantees version-sync migration execution, immutable ledger tracking (`content_sha256`), read-only schema drift verification, and live schema introspection to typed Pydantic Python or TypeScript models.

---

## Key Features

- **Multi-Engine Support**: Native migration engines for SQL Server (MSSQL), PostgreSQL (with session reset isolation), and SQLite (file-based).
- **Zero-Drift Migration Ledger**: Tracks applied migrations with SHA-256 content hashes in `dbtools_migration_history` alongside standard `schema_migrations` cursors.
- **Read-Only Drift Verification**: `dbtools verify` validates that live database objects match migration definitions and alerts when files were modified after execution.
- **Rollback & Down Migrations**: `dbtools down` applies `.down.sql` files in reverse; `dbtools rollback` is a ledger-only soft-revert — the safe prod verb. Destructive ops on protected targets require `--preview --yes`.
- **Agent-First Ergonomics**: Terraform-style exit-code contract (`0` clean / `1` error / `2` pending changes or drift), `--dry-run` previews, universal `--json`, and `DBTOOLS_NO_PROMPT=1` fail-closed mode for CI and AI agents. See [docs/exit-codes.md](docs/exit-codes.md).
- **Environment & Target Protection**: Targets are defined in `dbtools.toml` by environment variable names (`url_env`), ensuring secrets never leak into version control. Protected targets reject destructive operations (`up`, `reset`, and `down` without `--preview --yes`).
- **Python & TypeScript Type Generation**: `dbtools generate` introspects live database schemas and emits clean, versioned `pydantic.BaseModel` classes or Supabase-style TypeScript interfaces (+ optional zod schemas) for services, ETL jobs, and data pipelines.
- **Interactive TUI Dashboard**: Built-in terminal dashboard powered by Bubble Tea for real-time migration observability.

---

## Installation

### Via npm (Recommended for JS/TS users)

```bash
npx dbtools-cli status
```

or install globally:

```bash
npm install -g dbtools-cli
```

### Via Go Install

```bash
go install github.com/seanpham99/dbtools@latest
```

Ensure `$GOPATH/bin` or `$HOME/go/bin` is in your system `PATH`.

### From Source

```bash
git clone https://github.com/seanpham99/dbtools.git
cd dbtools
go build -o dbtools .
```

---

## Quick Start

### 1. Initialize Project

Create a starter configuration file (`dbtools.toml`) and migration folder:

```bash
dbtools init
```

### 2. Configure Database Targets

In `dbtools.toml`, define your database targets. Target URLs are mapped through environment variables:

```toml
migrations_dir = "migrations"

[targets.local]
url_env = "DBTOOLS_LOCAL_URL"
engine = "sqlite" # sqlite, postgres, or mssql

[targets.prod]
url_env = "DBTOOLS_PROD_URL"
protected = true
```

Set your connection string:

```bash
export DBTOOLS_LOCAL_URL="sqlite://dev.db"
# or for postgres: export DBTOOLS_LOCAL_URL="postgres://user:pass@localhost:5432/mydb?sslmode=disable"
# or for mssql:    export DBTOOLS_LOCAL_URL="mssql://sa:Secret@localhost:1433?database=mydb&TrustServerCertificate=true"
```

### 3. Create and Apply Migrations

```bash
# Scaffold new migration file ({timestamp}_create_users.up.sql)
dbtools new create_users

# Edit migrations/YYYYMMDDHHMMSS_create_users.up.sql with your DDL
echo "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL UNIQUE);" > migrations/*_create_users.up.sql

# Apply migrations to local target
dbtools up

# Inspect applied status
dbtools status
```

---

## CLI Command Reference

| Command | Usage | Description |
|---|---|---|
| `init` | `dbtools init` | Creates `dbtools.toml` configuration and `migrations/` directory. |
| `new` | `dbtools new <name>` | Scaffolds timestamped `.up.sql` migration file. |
| `up` | `dbtools up [--target <name>]` | Applies pending migrations to local target. Refuses protected/remote targets. |
| `push` | `dbtools push <target> [--yes]` | Applies pending migrations to named remote target with explicit confirmation. |
| `status` | `dbtools status [target] [--json]` | Displays applied/pending migration status across configured targets (`[unconfigured]` for unset env vars). |
| `doctor` | `dbtools doctor [target] [--json]` | Strictly read-only health, integrity, drift, and security audit. Exit 0 healthy / 1 error / 2 issues. |
| `plan` | `dbtools plan [--target X] [--json]` | Read-only preview of pending migrations + ledger drift, without applying anything. Agent/CI-friendly: exit 0 = safe to apply. |
| `verify` | `dbtools verify <target> [--json]` | Non-destructive verification of ledger history and live database objects. Exit 0 clean / 1 error / 2 drift (content-hash mismatch or missing object). |
| `down` | `dbtools down <target> [N] [--preview] [--yes]` | Applies `.down.sql` migrations in reverse order, recorded in the ledger. Protected targets require `--preview --yes`. |
| `rollback` | `dbtools rollback <target> [--yes]` | Ledger-only soft-revert (marks `reverted`, never data-destroying). The safe prod verb. |
| `repair` | `dbtools repair <target> <v>:<status> --yes` | Corrects ledger state (`applied`/`reverted`) and resynchronizes the version cursor. |
| `force` | `dbtools force <version> [--target <target>] [--yes]` | Sets tracking version cursor and clears dirty state without running migration SQL. |
| `reset` | `dbtools reset [target] [--yes]` | Unprotected targets: drops database, replays all migrations from zero, and executes `seed.sql`. |
| `generate` | `dbtools generate [target] [--lang python\|ts] [--zod] [--out file]` | Introspects live schema and renders Pydantic v2 models (`python`, default) or Supabase-style TypeScript interfaces (`ts`; `--zod` adds zod schemas). |
| `lint` | `dbtools lint [--dir <path>] [--json]` | Validates filenames, duplicate versions, and empty files without database connection. |
| `dashboard` | `dbtools dashboard` | Opens terminal UI showing live target status (`r` to refresh, `q` to quit). |
| `start` / `stop` | `dbtools start [--timeout 30s] [--no-wait]` / `stop` | Starts or stops ephemeral tool-owned local database Docker container with readiness polling. |

---

## Terminal Observability

### Status Overview

```text
Target: local (sqlite://dev.db)
  [APPLIED] 20260101000000_create_users.up.sql (SHA: a1b2c3d4)
  [PENDING] 20260102000000_add_orders.up.sql

Target: prod (DBTOOLS_PROD_URL) [PROTECTED]
  [APPLIED] 20260101000000_create_users.up.sql
  [PENDING] 20260102000000_add_orders.up.sql
```

### Interactive Dashboard

Launch with `dbtools dashboard`:

```text
┌───────────────────────────────────────────────────────────┐
│ dbtools migration status                                  │
│ Target: local [sqlite]                   Status: UP TO DATE │
│ Target: prod  [postgres]                 Status: 1 PENDING  │
│                                                           │
│ [r] Refresh    [q] Quit                                  │
└───────────────────────────────────────────────────────────┘
```

---

## Schema Drift & Ledger Verification

Traditional migration runners maintain only a single scalar version cursor. When migrations are tampered with or objects are altered outside the migration workflow, silent failures occur during deployment.

`dbtools` records:
1. Migration version timestamp
2. Applied status (`applied` / `reverted`)
3. SHA-256 digest of migration file content at execution time
4. Execution timestamp and execution context note

Running `dbtools verify <target>` inspects whether:
- Every applied migration file's content still matches its recorded cryptographic hash.
- Every database object (table, view) created by applied migrations actually exists in the database schema.
- Drop operations from subsequent migrations correctly excuse dropped tables without falsely flagging drift.

---

## Python Pydantic Model Generation

Automatically generate type-safe Pydantic models directly from your live database schema:

```bash
dbtools generate local --out models.py
```

Generated `models.py`:

```python
# Auto-generated by dbtools generate. DO NOT EDIT.
from pydantic import BaseModel, ConfigDict
from typing import Optional
from datetime import datetime

class Users(BaseModel):
    model_config = ConfigDict(from_attributes=True)

    id: int
    email: str
    created_at: Optional[datetime] = None
```

In CI/CD, enforce that repository models stay in sync with schema migrations using `--check`:

```bash
dbtools generate local --out models.py --check
```

---

## TypeScript Generation

Supabase-style type generation for JS/TS services:

```bash
# TypeScript interfaces
dbtools generate local --lang ts --out models.ts

# TypeScript interfaces + zod schemas
dbtools generate local --lang ts --zod --out models.ts
```

Generated `models.ts`:

```typescript
// Code generated by dbtools generate. DO NOT EDIT.
import { z } from "zod";

export interface Users {
  id: number;
  email: string;
  created_at?: string | null; // nullable columns become optional + null
}

export const UsersSchema = z.object({
  id: z.number(),
  email: z.string(),
  created_at: z.string().nullish(),
});
```

`--check` works the same for TS output (CI drift detection).

---

## Agent & CI Ergonomics

`dbtools` is designed to be called by AI agents and CI pipelines without interactive prompts:

- **Stable exit-code contract**: `0` clean, `1` error, `2` drift/pending changes. See [docs/exit-codes.md](docs/exit-codes.md).
- **`--dry-run`**: preview SQL without applying (`dbtools up --dry-run`).
- **Universal `--json`**: machine-readable output on stdout for `status`, `plan`, `verify`, and friends.
- **`DBTOOLS_NO_PROMPT=1`**: never block on stdin; destructive ops fail closed. Also auto-enabled when stdout is not a TTY.
- **Dirty-ledger refusal**: `up` refuses to apply when the ledger is dirty (a previous apply failed partway), surfacing the failure as exit `2` rather than silently continuing.

Example agent loop:

```bash
if dbtools plan --target prod --json; then
  # exit 0: safe to apply
  dbtools push prod --yes --dry-run
fi
```

> Note: exit `2` from `plan` means pending migrations or drift — inspect `--json` output to distinguish before deciding to apply.

---

## Development & Testing

Run unit tests locally (no database engine required):

```bash
go test ./...
```

Run integration test suite:

```bash
# SQLite integration test (runs locally without server)
DBTOOLS_TEST_SQLITE_URL="sqlite:///tmp/dbtools-it.db" go test -tags=integration ./internal/engine/sqliteengine/... -count=1

# Full matrix test (MSSQL, PostgreSQL, SQLite)
go test -tags=integration ./...
```

---

## Contributing

Contributions are welcome! Please read [CONTRIBUTING.md](CONTRIBUTING.md) for details on our code of conduct, test requirements, and development workflow.

See [docs/roadmap.md](docs/roadmap.md) for the project's direction and planned features.

## License

`dbtools` is licensed under the [MIT License](LICENSE).
