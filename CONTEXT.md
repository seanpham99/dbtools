# db-tools

`dbtools` is a standalone Go CLI (wrapping `golang-migrate`) providing a Supabase-CLI-style local dev loop and migration authority for MSSQL/Postgres/future engines. Not a business bounded context — a dev-tooling component living at `tools/db-tools/`. See `docs/adr/016-dbtools-migration-authority.md` for why it exists and what it replaces.

## Language

**target**:
A named database environment (`local`, `staging`, `prod`, ...) declared in `dbtools.toml`. Its connection string is never stored in the file — the target instead names an environment variable (`url_env`) resolved at runtime.
_Avoid_: environment, connection string, database URL

**push**:
Applying migrations not yet recorded in a target's version table (version-sync). Does not inspect or reconcile the target's live schema.
_Avoid_: sync, deploy, schema sync

**version-sync**:
The guarantee `up`/`push` provide: a target's recorded migration history matches local migration files up to the latest one applied. Distinct from full schema-sync (verifying every column/type/constraint of live DB structure matches expectations), which `dbtools` still does not implement — that would require per-engine live-schema introspection and diffing, ruled out as a dialect-abstraction cost too large for this tool. `verify`/`repair` (see `docs/adr/021-dbtools-migration-ledger-and-drift-detection.md`) check a much narrower thing — whether the named objects a migration's DDL creates actually exist — which is not the same as schema-sync.
_Avoid_: "in sync" (always qualify — ambiguous on its own), calling `verify` full schema-sync (it isn't — see ADR-021)

**reset**:
Local-only operation: drop the ephemeral local database, recreate it, replay every migration from the start, then run `seed.sql` if present.
_Avoid_: rollback, restore

**seed.sql**:
The single, optional file at the project root, run unconditionally at the end of `reset` — after all migrations have replayed against an empty database. Not required to be idempotent, since `reset` always starts from empty.
_Avoid_: fixtures, seed scripts (plural — there is only ever one)

**ephemeral local database**:
The tool-owned, per-engine container `start` provisions from a template bundled inside `dbtools` itself. Independent of any consumer repo's own container orchestration — `dbtools start` is a narrower loop for schema iteration only, not a substitute for a repo's own full-stack local dev environment.

**dashboard**:
The read-only `bubbletea` view over `status`. It never mutates anything; `r` refreshes and `q`/`ctrl+c` quits.
_Avoid_: dev database, local db (ambiguous with a repo's own compose-managed instance)

**ledger**:
`dbtools_migration_history` — the per-migration `applied`/`reverted` record
`verify` and `repair` reason over. Additive alongside `golang-migrate`'s own
single-cursor `schema_migrations` table, not a replacement for it; see
ADR-021.
_Avoid_: migration history (ambiguous with `schema_migrations` itself), tracking table (also ambiguous)

**repair**:
Corrects the ledger's `applied`/`reverted` status for one or more versions in
a single call, then recomputes `golang-migrate`'s cursor as the highest
remaining applied version. Replaces `stamp`.
_Avoid_: stamp (removed — see ADR-021)

**generate**:
Introspects a target's live `INFORMATION_SCHEMA` and renders one Python
`pydantic.BaseModel` per base table — a generated, versioned type contract
for code consuming that schema, in place of raw dicts. Read-only against the
database (no ledger involved); writes only the output file. Carries no
domain knowledge of its own — table/column names come straight from the live
schema, PascalCased with no acronym special-casing (see `internal/generate/`).
_Avoid_: export, dump (this produces typed code, not a data export)
