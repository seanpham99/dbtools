# dbtools adopt

`dbtools adopt` imports an existing migration ledger from an incumbent tool (golang-migrate, Flyway, EF Core, Knex, Alembic, or a bespoke runner) into dbtools without re-running any migration SQL.

## Usage

```bash
# Preview adoption plan (dry run, default)
dbtools adopt <target>

# Write imported records to the dbtools ledger and stamp cursor
dbtools adopt <target> --yes

# Proceed even if orphan records exist (source table rows with no matching file)
dbtools adopt <target> --yes --force

# Adopt from a bespoke or non-standard table
dbtools adopt <target> --from-table custom_history --version-column version_num [--applied-at-column applied_on] --yes
```

## How It Works

1. **Source Table Detection**:
   When `--from-table` is omitted, dbtools automatically probes for candidate migration tables in order:
   - `schema_migrations` (golang-migrate, Rails)
   - `flyway_schema_history` (Flyway)
   - `__EFMigrationsHistory` (Entity Framework Core)
   - `knex_migrations` (Knex.js)
   - `alembic_version` (Alembic)
   - `SchemaVersions` (DbUp)

2. **3-Way Diff**:
   Compares source table records against local migration files in `migrations_dir`:
   - **Matched**: Version present in both database and local directory -> imported.
   - **Pending**: Migration file exists on disk, but not recorded in source table -> remains pending for future `dbtools up`/`push`.
   - **Orphan**: Version recorded in source table, but no migration file on disk -> blocks adoption unless `--force` is passed.

3. **Orphan Safety Gate**:
   If orphan history rows are found, `dbtools adopt` exits with code `1` and writes nothing. Passing `--force` overrides the block and imports only the matched versions (orphans are skipped).

4. **Hash Source (`adopted`)**:
   Imported records are stored in the ledger with `hash_source = 'adopted'`. `dbtools doctor` and `dbtools verify` recognize these rows and skip content hash comparison (since historical file content was never directly observed at original execution time).

## Configuration

If your project uses custom table names or flat migration suffixes, configure them in `dbtools.toml`:

```toml
migrations_dir = "migrations"

[ledger]
table = "my_custom_migration_history" # Default: "dbtools_migration_history"

[migrations]
up_suffix = ".sql"                    # Default: ".up.sql" (supports ".sql" for flat layouts)
```
