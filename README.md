# dbtools

[![CI](https://github.com/seanpham99/dbtools/actions/workflows/ci.yml/badge.svg)](https://github.com/seanpham99/dbtools/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/seanpham99/dbtools.svg)](https://pkg.go.dev/github.com/seanpham99/dbtools)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**dbtools** is a lightweight, reliable database migration authority and local dev-loop tool for **MSSQL**, **PostgreSQL**, and **SQLite**. Built in Go for high performance and zero external runtime dependencies, `dbtools` guarantees version-sync migration execution, immutable ledger tracking (`content_sha256`), read-only schema drift verification, and live schema introspection to typed Pydantic Python models.

---

## Key Features

- **Multi-Engine Support**: Native migration engines for SQL Server (MSSQL), PostgreSQL (with session reset isolation), and SQLite (file-based).
- **Zero-Drift Migration Ledger**: Tracks applied migrations with SHA-256 content hashes in `dbtools_migration_history` alongside standard `schema_migrations` cursors.
- **Read-Only Drift Verification**: `dbtools verify` validates that live database objects match migration definitions and alerts when files were modified after execution.
- **Environment & Target Protection**: Targets are defined in `dbtools.toml` by environment variable names (`url_env`), ensuring secrets never leak into version control. Protected targets reject destructive local operations (`up`, `reset`).
- **Python Type Generation**: `dbtools generate` introspects live database schemas and emits clean, versioned `pydantic.BaseModel` classes for Python services, ETL jobs, and data pipelines.
- **Interactive TUI Dashboard**: Built-in terminal dashboard powered by Bubble Tea for real-time migration observability.

---

## Installation

### Via Go Install (Recommended)

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
| `status` | `dbtools status [--json]` | Displays applied/pending migration status across all configured targets. |
| `plan` | `dbtools plan [--target X] [--json]` | Read-only preview of pending migrations + ledger drift, without applying anything. Agent/CI-friendly: exit 0 = safe to apply. |
| `verify` | `dbtools verify <target> [--json]` | Non-destructive verification of ledger history and live database objects. Non-zero exit on drift. |
| `repair` | `dbtools repair <target> <v>:<status> --yes` | Corrects ledger state (`applied`/`reverted`) and resynchronizes the version cursor. |
| `reset` | `dbtools reset [--target local] [--yes]` | Local-only: drops database, replays all migrations from zero, and executes `seed.sql`. |
| `generate` | `dbtools generate [target] [--out models.py]` | Introspects live schema and renders Pydantic v2 Python models. |
| `lint` | `dbtools lint [--dir <path>] [--json]` | Validates filenames, duplicate versions, and empty files without database connection. |
| `dashboard` | `dbtools dashboard` | Opens terminal UI showing live target status (`r` to refresh, `q` to quit). |
| `start` / `stop` | `dbtools start` / `stop` | Starts or stops ephemeral tool-owned local MSSQL Docker container. |

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
