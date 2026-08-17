# dbtools

Migration authority and local dev-loop for MSSQL (Postgres planned) — see
`docs/adr/016-dbtools-migration-authority.md` for why this exists and
`CONTEXT.md` for the terms it introduces (`target`, `push`, `version-sync`,
`reset`, `seed.sql`).

## Commands (v1: core engine)

- `dbtools init` — create a starter `dbtools.toml` and `migrations/` directory.
- `dbtools new <name>` — scaffold a new `{timestamp}_{name}.up.sql` migration file.
- `dbtools up [--target local]` — apply pending migrations to a target.
- `dbtools push <target>` — apply pending migrations to a named target (version-sync only — same operation as `up`, explicit-by-name for remote targets).
- `dbtools start` — start the tool-owned ephemeral local MSSQL container and record its local connection URL.
- `dbtools stop` — stop and remove the tool-owned local MSSQL container.
- `dbtools reset` — drop, recreate, and replay the local database, then run `seed.sql` if present.
- `dbtools status [--json]` — show applied/pending state for every configured target.
- `dbtools verify <target> [--json]` — check every ledger-tracked migration's objects actually exist (or don't, for reverted versions); non-zero exit on drift. See `docs/adr/021-dbtools-migration-ledger-and-drift-detection.md`.
- `dbtools repair <target> <version>:<status>[,...] --yes [--force]` — correct the migration ledger's applied/reverted status for one or more versions; refuses to mark a version applied when its objects don't exist unless `--force`. Replaces the old `stamp` command.
- `dbtools dashboard` — read-only TUI showing every target's status; `r` refreshes and `q`/`ctrl+c` quits.
- `dbtools generate [target] [--out path] [--yes] [--check]` — introspect a target's live schema and render one `pydantic.BaseModel` per base table to a Python file. `--yes` required when target is `prod`. `--check` compares against the file already on disk and exits non-zero on drift instead of writing (CI use). See "Generate" below.

## Config

`dbtools.toml` at the project root:

```toml
migrations_dir = "migrations"

[targets.local]
url_env = "DBTOOLS_LOCAL_URL"

[targets.prod]
url_env = "DBTOOLS_PROD_URL"
```

No target's connection string is ever written in this file — set the named
environment variable before running `up`/`push`/`status` against that target.

## Generate

`dbtools generate` introspects `INFORMATION_SCHEMA` (MSSQL only today) for a
target and renders one `pydantic.BaseModel` per base table, giving consuming
Python code (services, ETL scripts, tests) a versioned type contract to
`model_validate()` against instead of raw dicts. It ships with zero domain
knowledge — table/column names come straight from the live schema, and Python
class names are the schema's table names PascalCased (no acronym handling —
`cpi_index` becomes `CpiIndex`, not `CPIIndex`).

Configurable via an optional `[generate]` block in `dbtools.toml`, entirely
up to the consuming project:

```toml
[generate]
exclude = ["dbtools_migration_history", "schema_migrations"]  # table names never to generate a model for
out = "models.py"                                              # default --out path
```

If `exclude` is omitted entirely, it defaults to `["dbtools_migration_history",
"schema_migrations"]` (dbtools' own ledger tables) — set `exclude` explicitly
(even to `[]`, to include everything) to override that default rather than add
to it. See `internal/generate/` for the introspection/rendering code.

## Development

```bash
go build ./...
go test ./...                        # unit tests, no DB required
go test -tags=integration ./...      # requires DBTOOLS_TEST_MSSQL_URL (see .github/workflows/ci.yml)
```
