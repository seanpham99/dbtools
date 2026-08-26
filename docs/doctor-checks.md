# dbtools doctor Checks & Exit Codes

`dbtools doctor [target] [--json]` is a strictly **read-only** health, integrity, and security auditing command for target databases.

It never modifies the database, creates tables, writes to the ledger, or mutates state.

---

## Exit-Code Contract

`dbtools doctor` communicates health status via process exit codes for integration with CI pipelines, deploy gates, and AI coding agents:

| Exit Code | Meaning | Details |
|---|---|---|
| **`0`** | **HEALTHY** | All checks passed (`[OK]`, `[WARN]`, or `[SKIP]`). Database is up to date, ledger is intact (or gracefully skipped), and credentials are safe. |
| **`1`** | **ERROR** | Unreachable target, database connection failure, invalid configuration, or unreadable migrations directory. |
| **`2`** | **ISSUES DETECTED** | Health/integrity violations found: schema drift detected, content-hash mismatch, pending migrations, or dirty ledger state. |

---

## Check Reference

`dbtools doctor` executes 6 independent diagnostic checks:

### 1. `connectivity`
- Resolves the target's connection string from its configured `url_env` environment variable.
- Confirms the database engine is registered (`sqlite`, `postgres`, `mssql`, `mysql`).
- Establishes a connection and pings the database server.
- **Fail (`exit 1`)**: Connection refused, bad credentials, timeout, or unknown target.

### 2. `ledger-integrity`
- Inspects the `dbtools_migration_history` ledger table.
- For each applied migration with a recorded SHA-256 hash, verifies the migration file on disk matches the hash recorded when applied.
- **Skipped (`[SKIP]`)**: No ledger table exists (ledger-free mode — run `dbtools adopt` to enable).
- **Warn**: Ledger table exists but is empty.
- **Fail (`exit 2`)**: Content hash mismatch (migration file edited post-apply) or corrupted history.

### 3. `version-sync`
- Compares the live database migration version against `.up.sql` files in `migrations_dir`.
- Detects pending migrations that need to be applied.
- **Fail (`exit 2`)**: One or more pending migrations detected.
- **OK**: Database is fully up to date (appends `(no dbtools ledger)` when ledger table is absent) or cleanly unmigrated.

### 4. `drift-summary`
- Inspects live database schema objects (tables, functions) created by applied migrations.
- In ledger-free mode: walks migration files directly to verify object presence without requiring a recorded hash.
- **Warn**: Skipped if ledger exists but is empty.
- **Fail (`exit 2`)**: Schema drift detected (applied objects missing or dropped).

### 5. `dirty-ledger`
- Reads the `dirty` flag from migration version tracking.
- Surfaces partial migration failures where a previous migration failed partway through execution.
- **Skipped (`[SKIP]`)**: No ledger table and no migration cursor recorded.
- **Fail (`exit 2`)**: Target is marked `dirty=true`.

### 6. `security-flags`
- Verifies connection strings are sourced from environment variables (`url_env`) rather than plaintext in config.
- Reports target protection status (`protected=true` vs `protected=false`).
- Validates the migrations directory exists and contains no duplicate versions or malformed files.
- **Warn**: Plaintext URLs or missing directories.

---

## Example Usage & Output

### Human Mode (Single Target)
```bash
$ dbtools doctor local
Target: local (sqlite)
  [OK]    connectivity       connected to database (sqlite)
  [OK]    ledger-integrity   4 ledger entries verified (hashes match)
  [OK]    version-sync       up to date (version 20260822000004)
  [OK]    drift-summary      no schema drift detected
  [OK]    dirty-ledger       ledger clean (dirty=false)
  [OK]    security-flags     url_env=DBTOOLS_LOCAL_URL, protected=false
Result: HEALTHY (exit 0)
```

### Human Mode (All Targets with Issues)
```bash
$ dbtools doctor
Target: local (sqlite)
  [OK]    connectivity       connected to database (sqlite)
  [OK]    ledger-integrity   4 ledger entries verified (hashes match)
  [OK]    version-sync       up to date (version 20260822000004)
  [OK]    drift-summary      no schema drift detected
  [OK]    dirty-ledger       ledger clean (dirty=false)
  [OK]    security-flags     url_env=DBTOOLS_LOCAL_URL, protected=false
Result: HEALTHY (exit 0)

Target: staging (postgres)
  [OK]    connectivity       connected to database (postgres)
  [FAIL]  ledger-integrity   content hash mismatch in 1 migration(s)
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
  "engine": "sqlite",
  "healthy": true,
  "exit": 0,
  "checks": [
    {"name": "connectivity", "status": "ok", "message": "connected to database (sqlite)"},
    {"name": "ledger-integrity", "status": "ok", "message": "4 ledger entries verified (hashes match)"},
    {"name": "version-sync", "status": "ok", "message": "up to date (version 20260822000004)"},
    {"name": "drift-summary", "status": "ok", "message": "no schema drift detected"},
    {"name": "dirty-ledger", "status": "ok", "message": "ledger clean (dirty=false)"},
    {"name": "security-flags", "status": "ok", "message": "url_env=DBTOOLS_LOCAL_URL, protected=false"}
  ]
}
```
