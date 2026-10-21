# dbtools doctor Checks & Exit Codes

`dbtools doctor [target] [--json]` is a strictly **read-only** health, integrity, and security auditing command for target databases.

It never modifies the database, creates tables, writes to the ledger, or mutates state.

---

## Exit-Code Contract

`dbtools doctor` communicates health status via process exit codes for integration with CI pipelines, deploy gates, and AI coding agents:

| Exit Code | Meaning | Details |
|---|---|---|
| **`0`** | **HEALTHY** | No check failed (`[OK]`, `[WARN]`, and `[SKIP]` all exit 0). Database is up to date, ledger is intact (or gracefully skipped), and credentials are safe. |
| **`1`** | **ERROR** | Unreachable target, database connection failure, invalid configuration, or unreadable migrations directory. |
| **`2`** | **ISSUES DETECTED** | Health/integrity violations found: schema drift detected, content-hash mismatch, pending migrations, or dirty ledger state. |

A warning never changes the exit code. Checks that can only be advisory — a server version outside the tested window, a local development database on a different major — report `[WARN]` and leave the target healthy.

---

## Check Reference

Which checks run depends on the target: `server-version` needs an engine that reports a version, and `local-version` needs the project to have a postgres local database.

### 1. `connectivity`
- Resolves the target's connection string from its configured `url_env` environment variable.
- Confirms the database engine is registered (`sqlite`, `postgres`, `mssql`, `mysql`).
- Establishes a connection and pings the database server.
- **Fail (`exit 1`)**: Connection refused, bad credentials, timeout, or unknown target.

### 2. `ledger-integrity`
- Inspects the `dbtools_migration_history` ledger table.
- For each applied migration with a recorded SHA-256 hash, verifies the migration file on disk matches the hash recorded when applied.
- **Skipped (`[SKIP]`)**: No ledger table exists (ledger-free mode — run `dbtools adopt` to enable).
- **OK**: Ledger table exists but is empty.
- **Fail (`exit 2`)**: Content hash mismatch (migration file edited post-apply) or corrupted history.

### 3. `server-version`
- Compares the target's reported server version against the window dbtools is tested on (`internal/support`): Postgres 15–18, MySQL 8.0 and 8.4, SQL Server 2019 and 2022.
- **OK**: the version is inside the tested range.
- **Warn**: the version is outside it. dbtools still runs; version-specific behaviour is simply not covered by its test suite.
- Absent for engines that report no server version (SQLite).

### 4. `local-version`
- Reports the major version of the project's local development database next to the target's.
- The major reported is the one the local database actually has, read from what the provider leaves on disk: the docker data volume's recorded image, or the pgx runtime's own major. Before a local database exists the configured major is reported as a prediction ("not created yet").
- **OK**: the two majors match, or the target's version is unknown.
- **Warn**: the majors differ. A schema developed against one major is rendered differently by the other, which is the class of difference that looks like drift.
- **Warn**: the local database's data belongs to a different major than `[local] postgres_major` now asks for — the pin has not been applied yet. Names the `dbtools stop --no-backup` / `dbtools start` pair that applies it.
- **Warn**: the configured `[local] postgres_major` cannot be served by the selected provider, so the local major cannot be determined.
- Only present when the project has a `local` target on the postgres engine. It never fails: the local database is a development convenience and cannot make an unrelated target's report unhealthy.

### 5. `version-sync`
- Compares the live database migration version against `.up.sql` files in `migrations_dir`.
- Detects pending migrations that need to be applied.
- **Fail (`exit 2`)**: One or more pending migrations detected.
- **OK**: Database is fully up to date (appends `(no dbtools ledger)` when ledger table is absent) or cleanly unmigrated.

### 6. `drift-summary`
- Inspects live database schema objects (tables, functions) created by applied migrations.
- In ledger-free mode: walks migration files directly to verify object presence without requiring a recorded hash.
- **Warn**: Skipped if ledger exists but is empty.
- **Fail (`exit 2`)**: Schema drift detected (applied objects missing or dropped). A name the same migration creates and then drops or renames away is a staging name and is not required to exist.

### 7. `dirty-ledger`
- Reads the `dirty` flag from migration version tracking.
- Surfaces partial migration failures where a previous migration failed partway through execution.
- **Skipped (`[SKIP]`)**: No ledger table and no migration cursor recorded.
- **Fail (`exit 2`)**: Target is marked `dirty=true`.

### 8. `security-flags`
- Verifies connection strings are sourced from environment variables (`url_env`) rather than plaintext in config.
- Reports target protection status (`protected=true` vs `protected=false`).
- Validates the migrations directory exists and contains no duplicate versions or malformed files.
- **Warn**: Plaintext URLs or missing directories.

---

## Example Usage & Output

### Human Mode (Single Target)
```bash
$ dbtools doctor local
Target: local (postgres)
  [OK]    connectivity       connected to database (postgres)
  [OK]    ledger-integrity   4 ledger entries verified (hashes match)
  [OK]    server-version     postgres 17 is within the tested range
  [OK]    local-version      local database: postgres 17 (docker provider), the same major as the target
  [OK]    version-sync       up to date (version 20260822000004)
  [OK]    drift-summary      no schema drift detected
  [OK]    dirty-ledger       ledger clean (dirty=false)
  [OK]    security-flags     url_env=DBTOOLS_LOCAL_URL, protected=false
Result: HEALTHY (exit 0)
```

### Human Mode (All Targets with Issues)
```bash
$ dbtools doctor
Target: local (postgres)
  [OK]    connectivity       connected to database (postgres)
  [OK]    ledger-integrity   4 ledger entries verified (hashes match)
  [OK]    server-version     postgres 17 is within the tested range
  [OK]    local-version      local database: postgres 17 (docker provider), the same major as the target
  [OK]    version-sync       up to date (version 20260822000004)
  [OK]    drift-summary      no schema drift detected
  [OK]    dirty-ledger       ledger clean (dirty=false)
  [OK]    security-flags     url_env=DBTOOLS_LOCAL_URL, protected=false
Result: HEALTHY (exit 0)

Target: staging (postgres)
  [OK]    connectivity       connected to database (postgres)
  [FAIL]  ledger-integrity   content hash mismatch in 1 migration(s)
  [OK]    server-version     postgres 18 is within the tested range
  [WARN]  local-version      local database: postgres 17 (docker provider); target: postgres 18 — a schema developed against one major is rendered differently by the other
  [FAIL]  version-sync       1 pending migration(s) (current version 20260822000003)
  [FAIL]  drift-summary      drift detected in 1 migration(s)
  [OK]    dirty-ledger       ledger clean (dirty=false)
  [OK]    security-flags     url_env=DBTOOLS_STAGING_URL, protected=false
Result: ISSUES DETECTED (exit 2)
```

### JSON Mode (`--json`)
```bash
$ dbtools doctor local --json
{
  "target": "local",
  "engine": "postgres",
  "healthy": true,
  "exit": 0,
  "checks": [
    {"name": "connectivity", "status": "ok", "message": "connected to database (postgres)"},
    {"name": "ledger-integrity", "status": "ok", "message": "4 ledger entries verified (hashes match)"},
    {"name": "server-version", "status": "ok", "message": "postgres 17 is within the tested range"},
    {"name": "local-version", "status": "ok", "message": "local database: postgres 17 (docker provider), the same major as the target"},
    {"name": "version-sync", "status": "ok", "message": "up to date (version 20260822000004)"},
    {"name": "drift-summary", "status": "ok", "message": "no schema drift detected"},
    {"name": "dirty-ledger", "status": "ok", "message": "ledger clean (dirty=false)"},
    {"name": "security-flags", "status": "ok", "message": "url_env=DBTOOLS_LOCAL_URL, protected=false"}
  ]
}
```
