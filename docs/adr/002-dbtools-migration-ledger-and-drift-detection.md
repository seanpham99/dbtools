# ADR 002: Migration Ledger Tracking and Read-Only Drift Detection

## Status
Accepted

## Context
Traditional migration runners (such as `golang-migrate`) store only a single scalar version number and dirty flag in a `schema_migrations` table. This approach introduces significant blind spots:
1. **Silent Post-Apply File Edits**: If a developer or automated process edits a migration file after it has been executed, the runner cannot detect the divergence.
2. **Untracked Out-of-Band Schema Changes**: If tables or views created by migrations are dropped, altered, or recreated outside the migration pipeline, the scalar version remains unchanged, masking schema corruption.
3. **Dirty Cursor Confusion**: When a migration fails partway through, manually marking it as applied (`stamp`) without verifying live schema objects leads to broken downstream deployments.

## Decision
We introduce a structured migration ledger (`dbtools_migration_history`) and a non-destructive drift verification system (`dbtools verify` and `dbtools repair`).

### 1. The Migration Ledger (`dbtools_migration_history`)
Alongside the native `schema_migrations` table, `dbtools` maintains an audit ledger:

```sql
CREATE TABLE dbtools_migration_history (
    version BIGINT NOT NULL PRIMARY KEY,
    status VARCHAR(20) NOT NULL CHECK (status IN ('applied', 'reverted')),
    recorded_at TIMESTAMP NULL,
    note VARCHAR(255) NULL,
    content_sha256 VARCHAR(64) NULL
);
```

- When a migration is applied, its raw file content is hashed (SHA-256) and recorded with status `applied`.
- When reverted, status is updated to `reverted`.

### 2. Read-Only Drift Verification (`dbtools verify`)
`dbtools verify <target>` inspects target databases without executing mutations:
1. **Content Hash Verification**: Compares the cryptographic hash of local migration files against `content_sha256` recorded in the ledger. Flagged as `DRIFT` if modified.
2. **Object Existence Verification**: Uses AST/regex DDL extractors to identify named schema objects (tables, views) created in migration scripts and queries the database catalog (`INFORMATION_SCHEMA` or `sqlite_master`).
3. **Drop Lifecycle Reasoning**: If an applied migration created an object, but a subsequent applied migration explicitly executed a `DROP`, the object's absence is excused as intentional rather than flagged as drift. If an object is subsequently recreated and later goes missing, drift is correctly flagged.

### 3. Repair Semantics (`dbtools repair`)
Replaces the old `stamp` command. `dbtools repair <target> <version>:<status> --yes` allows operators to rectify ledger rows while enforcing that objects claimed to be `applied` actually exist in the database (unless overridden by `--force`).

## Consequences
### Positive
- Cryptographic proof that live databases reflect exact migration file contents.
- Instant detection of accidental manual table drops or modifications.
- Non-destructive execution safe to run in automated CI/CD monitoring and health checks.

### Negative
- Extra table overhead (`dbtools_migration_history`) created in target databases.
- DDL parsing is limited to object definitions and drops (does not perform full column-level AST diffing).
