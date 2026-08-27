# dbtools squash

`dbtools squash <target>` collapses a long migration history into a single, verified baseline migration file, safe on both fresh databases and already-migrated targets.

## The Dual-Population Problem

Collapsing migration history manually is notoriously error-prone because a baseline must satisfy two conflicting environments:

1. **Fresh databases** (e.g., new developers, CI test instances): Must apply the single baseline file (`0000000000000_squashed_baseline.up.sql`) to bring up the full schema quickly.
2. **Already-migrated databases** (e.g., staging, prod, local dev targets): Have already executed the collapsed migrations. They must **never** re-execute the baseline DDL (which would cause duplicate table errors), and their subsequent `dbtools up`/`push` must not break when the old migration files are archived.

## The `versionExists` Constraint & Re-stamping

`golang-migrate` (the migration runner) checks `versionExists(currentCursorVersion)` before stepping. If a database cursor sits at version `N`, but migration file `N` has been deleted or archived, the next `dbtools up` hard-errors with `"no migration found for version N"`.

To prevent this:
- When run with `--yes` against an already-applied target (`current_version >= upto_version`), `dbtools squash` automatically re-stamps that target's cursor to version `0` (`m.Stamp(0)`) and writes a verified ledger entry with status `applied`.
- The next `dbtools up` on that target finds version `0` already satisfied and proceeds smoothly without re-executing any SQL.

## Usage

```bash
# Preview squash plan and verify equivalence (dry-run by default)
dbtools squash local

# Collapse migrations up to a specific version (dry-run)
dbtools squash local --upto 20260822000010

# Execute the squash: write baseline, archive files, and re-stamp target
dbtools squash local --yes

# Specify a custom baseline output filename
dbtools squash local --out 0000000000000_init_schema.up.sql --yes
```

## How It Works

1. **Replay into Scratch DB #1**: Replays migration files up to `--upto` into a temporary, isolated scratch database (`internal/scratchdb`).
2. **Native Schema Dump**: Dumps the schema using the native host tool (`pg_dump`, `mysqldump`, `mssql-scripter`, or direct SQLite catalog query).
3. **Verify in Scratch DB #2**: Applies the dumped baseline SQL directly into a fresh second scratch database and runs `diff.Compare` between Scratch #1 and Scratch #2. If any structural difference is detected, squash aborts before writing anything.
4. **Write Baseline & Archive**:
   - Writes `0000000000000_squashed_baseline.up.sql` into `migrations/`.
   - Moves collapsed migration files (`.up.sql` and `.down.sql`) into `migrations/_archived/` (preserving history for audits rather than deleting).
5. **Re-stamp Target**:
   - If target is **fresh** (`!has_version`): Leaves cursor untouched.
   - If target is **fully applied** (`version >= upto`): Stamps cursor to `0` and records an applied ledger entry with the baseline SHA-256 hash.
   - If target is **partially applied** (`0 < version < upto`): Refuses to proceed (operator must finish applying or choose a smaller `--upto`).

## Native Tool Dependencies & Install Hints

- **PostgreSQL**: Requires `pg_dump` on `PATH` (install `postgresql-client`). Post-processing automatically strips search path resets and notice suppressions.
- **MySQL**: Requires `mysqldump` on `PATH` (install `mysql-client` or `mariadb-client`).
- **MSSQL**: Requires `mssql-scripter` on `PATH` (install via `pip install mssql-scripter`).
- **SQLite**: No external tool required (queries `sqlite_master` directly).

## Deploying to Other Environments (Manual Follow-up)

`dbtools squash` modifies only the `<target>` provided on the command line (blast radius is single-target).

When deploying the squashed codebase to other already-migrated environments (such as staging or production), run:

```bash
dbtools repair <target> 0:applied --yes
```

This marks version `0` as applied in that environment's ledger, ensuring it will not attempt to run the squashed baseline. Fresh environments do not need this — they simply run `dbtools up` or `dbtools push` to apply the baseline.
