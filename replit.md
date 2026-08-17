# dbtools

A Go CLI for MSSQL/Postgres database migration management and local dev-loop automation. Used as a hub for AI agents to interact with multiple database targets.

## Stack

- **Language:** Go 1.25
- **Key deps:** `golang-migrate/migrate`, `microsoft/go-mssqldb`, `charmbracelet/bubbletea` (TUI), `spf13/cobra`

## Building

```bash
go build -o dbtools .
```

The `Build dbtools` workflow does this automatically on start.

## Commands

```bash
./dbtools init                  # create dbtools.toml + migrations/ directory
./dbtools new <name>            # scaffold a new migration file
./dbtools up [--target local]   # apply pending migrations
./dbtools push <target>         # push to a named remote target
./dbtools status [--json]       # show applied/pending state per target
./dbtools verify <target>       # check for ledger drift
./dbtools repair <target> ...   # fix ledger status
./dbtools dashboard             # read-only TUI (r=refresh, q=quit)
./dbtools generate [target]     # emit pydantic BaseModels from live schema
./dbtools reset                 # drop + replay local DB + seed.sql
```

## Configuration

`dbtools.toml` at the project root (created by `dbtools init`):

```toml
migrations_dir = "migrations"

[targets.local]
url_env = "DBTOOLS_LOCAL_URL"
engine = "mssql"  # optional; defaults from the URL scheme and must match it

[targets.prod]
url_env = "DBTOOLS_PROD_URL"
```

Connection strings are **never** stored in the file — set the named env var (Replit Secret) before running commands against that target.

## Environment Variables / Secrets

| Secret | Purpose |
|--------|---------|
| `DBTOOLS_LOCAL_URL` | Local/dev MSSQL connection string |
| `DBTOOLS_PROD_URL` | Production MSSQL connection string |

Connection string format: `mssql://user:password@host:1433?database=mydb&TrustServerCertificate=true`

> **Note:** `dbtools start/stop` use Docker to spin up a local MSSQL container. Docker is not available on Replit — point `DBTOOLS_LOCAL_URL` at an external MSSQL instance (Azure SQL, a self-hosted VM, etc.) instead.

## Running Tests

```bash
go test ./...                           # unit tests (no DB required)
go test -tags=integration ./...         # integration tests (needs DBTOOLS_TEST_MSSQL_URL)
```

## User Preferences

- This project is used as a hub for AI agents to manage schema migrations across multiple databases (MSSQL, Postgres, SQLite).
