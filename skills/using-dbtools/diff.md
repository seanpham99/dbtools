# dbtools diff

`dbtools diff` replays all local migration files into a fresh, isolated scratch database and compares its structural schema catalog against a live target database to detect manual hotfixes and out-of-band schema changes.

## Why `diff` Exists (The Ledger Blind Spot)

`dbtools verify` and `dbtools doctor` check whether the live database schema matches what the migration **ledger** recorded. If a developer or DBA executes an out-of-band `ALTER TABLE` or `CREATE TABLE` directly against production at 2am (a manual hotfix), the migration ledger remains untouched and clean. `verify` cannot see this drift because no ledger entry was created or modified.

`dbtools diff` answers the fundamental question:
> **"Does the live database schema structurally match what these migration files would produce if executed from scratch today?"**

## Usage

```bash
# Compare live target against a fresh scratch database (auto-provisioned)
dbtools diff <target>

# Compare against an already-provisioned, empty scratch database
dbtools diff <target> --against "postgres://postgres:pass@localhost:5432/scratch_db?sslmode=disable"

# Emit machine-readable JSON output for CI / agent assertions
dbtools diff <target> --json
```

## How It Works

1. **Scratch Database Provisioning**:
   - **SQLite**: Creates an ephemeral temporary file (`os.CreateTemp`), removes it on cleanup.
   - **Postgres / MySQL / MSSQL**: Automatically spins up an ephemeral, `--rm` Docker container on a random port with a dedicated scratch naming scheme (`dbtools-diff-scratch-<engine>-<timestamp>`), waits for readiness, and stops it when done.
   - **`--against <url>`**: Replays directly into a caller-supplied database, skipping Docker provisioning entirely.

2. **Migration Replay**:
   Replays all migration files from `migrations_dir` into the scratch database using the exact same `apply.Run` path used by `dbtools up` and `dbtools push`.

3. **Full Catalog Introspection**:
   Introspects both the scratch database and the live target database across all structural objects (tables, columns, primary keys, foreign keys, non-PK indexes, CHECK constraints, and defaults).

4. **Structural Comparison**:
   Compares the catalog and classifies differences:
   - **`MISSING`**: Present in scratch (defined in migrations) but missing in the live target (e.g. a table or column was dropped out-of-band).
   - **`EXTRA`**: Present in the live target but not defined in any migration file (the manual hotfix case).
   - **`CHANGED`**: Present in both, but differs in data type, nullability, max length, precision, scale, default value, primary key, index uniqueness/columns, foreign key reference, or check constraint expression.

5. **Column Ordinal Position**:
   Differences in column ordering (`OrdinalPosition`) are reported purely as informational notes and are **not** treated as drift (they do not trigger exit code 2).

6. **CHECK Constraints**:
   Compared on Postgres, MySQL (8.0.16+), and MSSQL. SQLite does not have a queryable CHECK constraint catalog, so `CheckConstraints` is empty on both sides and naturally produces zero diffs.

## Exit Codes

- `0`: Clean — live target schema matches migrations perfectly (no structural differences).
- `2`: Differences found — one or more `MISSING`, `EXTRA`, or `CHANGED` findings detected.
- `1`: Error — connection failure, replay error, or invalid configuration.

## Safety & Target Read-Only Contract

`dbtools diff` is strictly **read-only** against the target database. It only connects to the target to perform read-only metadata introspection. All migration replay and ledger writes happen exclusively inside the ephemeral scratch database.
