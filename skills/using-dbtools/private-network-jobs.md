# Private-Network Job Execution & Observability Reference

When databases reside in private subnets with no public ingress or direct developer access, database migrations and schema inspections run as container jobs inside the private network (e.g., Azure Container Apps Jobs, AWS ECS RunTask, Kubernetes Jobs, GCP Cloud Run Jobs).

`dbtools` is designed for headless, automated execution in these environments.

---

## 1. The Read-Only Observation Pattern (Low-Risk On-Ramp)

Before authorizing mutating migration jobs in production, run read-only observation jobs to inspect database state, preview pending migrations, or audit schema health without risking schema mutations or locks:

```bash
# Check applied and pending migration status across targets
dbtools status prod --json

# Preview pending migrations and drift without executing SQL
dbtools plan --target prod --json

# Run comprehensive health and integrity checks (connectivity, hash integrity, dirty state)
dbtools doctor prod --json
```

### Safety Guarantee
Read-only commands (`status`, `plan`, `doctor`, `verify`) never acquire DDL locks, never mutate ledger history, and never require `--yes`. They are safe to run periodically as scheduled audit jobs or pre-deployment inspection steps.

---

## 2. Mutating Migration Execution

To apply pending migrations inside a private network job:

```bash
# Push pending migrations to a named protected target
dbtools push prod --yes

# Or apply pending migrations in single-target container environments
dbtools up --yes
```

### Protection Gates
- Protected targets require `--yes` (or `DBTOOLS_NO_PROMPT=1` / `CI=1` alongside `--yes`).
- If `--yes` is omitted in automated/non-interactive jobs, `dbtools` fails closed immediately with exit code `1` rather than hanging on standard input.

---

## 3. Exit Codes & Container Retry Semantics

`dbtools` adheres strictly to Terraform-style exit codes:

| Exit Code | Meaning | Container Platform Action |
|:---:|---|---|
| `0` | **Success / Clean** | Job succeeded; no pending changes or drift. |
| `1` | **Fatal Error** | Job failed (network failure, syntax error, missing `--yes`, migration error). |
| `2` | **Pending Changes / Drift** | Inspection found unapplied migrations, schema drift, or dirty ledger. |

### Dirty Ledger Cursor Safety Under Orchestrator Retries

Container orchestrators often configure retry policies:
- **Azure Container Apps Jobs**: `replica_retry_limit > 0`
- **Kubernetes Jobs**: `backoffLimit > 0`
- **AWS ECS / Step Functions**: Retry on task failure

When a migration script fails partway through execution:
1. `dbtools` leaves the failed version's row in the ledger (`dbtools_migration_history`) with status `applying` — written before the migration ran, and never replaced because it did not finish.
2. The container exits with code `1`.
3. If the orchestrator automatically restarts or spawns a retry replica, `dbtools` inspects the ledger on startup, finds the surviving `applying` row, and **fails closed immediately** with exit code `1` — naming the migration that died, which a boolean dirty flag could not.

This prevents catastrophic partial-state replay or double-execution of non-idempotent DDL statements. The ledger stays blocked until an operator inspects the failure and runs `dbtools repair <target> <version>:<status> --yes` or `dbtools force <version> --yes`.

Concurrent executions are also excluded outright: every write path holds an engine-level advisory lock (`pg_advisory_lock`, `GET_LOCK`, `sp_getapplock`) for the whole run, so a job triggered twice serialises instead of interleaving DDL.

---

## 4. Structured Logging & Log Scraping

For ingestion into centralized log analytics (Azure Log Analytics / Container Insights, AWS CloudWatch, Datadog, Grafana Loki):

```bash
# Enable JSON structured logging via flag
dbtools push prod --yes --log-format=json

# Or enable JSON structured logging via environment variable
export DBTOOLS_LOG_FORMAT=json
dbtools push prod --yes
```

### Standard Output vs. Standard Error Separation
When combining `--json` (machine output) and `--log-format=json` (structured logs):
- **`stdout`**: Contains exclusively the machine-readable command result payload (JSON object).
- **`stderr`**: Contains structured operational logs, timestamped progress messages, and server notices.

This allows CI/CD orchestrators to parse `$STDOUT` with `jq` while log scrapers ingest `$STDERR` into log analytics.

### Job-Completion Summary Record

`dbtools` emits exactly **one** JSON document on stdout per `--json` run,
and completion status is folded into it. `up` (and `push` when already
current) carry `"ok": true` inside the status document itself; other
mutating commands print their result document as their last stdout line.

There is no separate deferred summary record. A log scraper can still
distinguish "the job ran to completion and reported its own outcome"
from "the job died mid-write" by document completeness: a killed process
never prints its status document at all, while a run that reached the
end of its control flow always has a complete, parseable JSON value —
the two failure modes look identical from the exit code alone, since an
orchestrator killing the container also reports a non-zero exit.

---

## 5. PostgreSQL Diagnostics in Detached Jobs

Debugging migration failures in headless container jobs requires detailed diagnostic context without interactive database access. `dbtools` includes built-in diagnostics for PostgreSQL:

### Server `NOTICE` Forwarding
PostgreSQL server notices (`RAISE NOTICE`, informational messages during `CREATE INDEX CONCURRENTLY`, table existence warnings) are forwarded in real time to stderr/logs instead of being silently swallowed by the driver.

### Character-Offset Error Translation
When a PostgreSQL migration fails with a syntax or semantic error, PostgreSQL returns a raw 1-based character position. `dbtools` automatically translates this character offset into:
- 1-based line and column coordinates
- The source line of SQL from the migration file
- A visual caret pointer (`^`) pinpointing the exact offending token

```
migration error at line 4, column 15:
  4 | CREATE TABEL invalid_syntax (id int);
    |               ^
pq: syntax error at or near "TABEL" (SQLSTATE 42601)
```

### Automated Permission Diagnostics (`SQLSTATE 42501`)
If a migration fails with `SQLSTATE 42501` (`insufficient_privilege`), `dbtools` queries database metadata to diagnose permission mismatches and emits an actionable diagnostic report:

```
[permission diagnostic (SQLSTATE 42501)]
  current_user: app_migrator
  session_user: app_migrator
  database: my_production_db
  schema: public (owner: postgres)
  schema privileges: USAGE=true, CREATE=false
  azure_pg_admin member: false
  remediation: User "app_migrator" lacks CREATE/USAGE privilege on schema "public". Run: GRANT USAGE, CREATE ON SCHEMA "public" TO "app_migrator";
```
