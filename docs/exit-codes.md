# dbtools Exit Codes & Agent Automation Contract

`dbtools` follows the Terraform `-detailed-exitcode` style for predictable scripting in CI pipelines and autonomous AI agent workflows.

## Process Exit Codes

| Code | Status | Meaning |
|:---:|---|---|
| `0` | **Success / Clean** | Command completed successfully with no pending changes or drift. |
| `1` | **Error** | Command encountered a fatal error (network failure, syntax error, missing config, invalid arguments, etc.). |
| `2` | **Pending Changes / Drift** | Command succeeded in inspection, but detected unapplied migrations or schema drift requiring attention. |

---

## Command Behavior Reference

### `dbtools plan`
- **Exit 0**: All configured targets are fully up to date with 0 pending migrations, 0 drift, and clean ledger cursors.
- **Exit 1**: Failed to reach one or more targets, invalid configuration, or execution failure.
- **Exit 2**: One or more targets have pending unapplied migrations, schema drift, dirty ledger cursors, or migration files that sit below the watermark and can never be applied (see [Below-watermark migration files](#below-watermark-migration-files)).

### `dbtools verify <target>`
- **Exit 0**: Database matches the migration ledger cleanly (all applied objects exist and hashes match).
- **Exit 1**: Failed to query database, ledger missing without `--init-ledger`, or database error.
- **Exit 2**: Drift detected (objects created by an applied migration are missing or content hash mismatch).

### `dbtools diff <target>`
- **Exit 0**: Live target schema matches scratch replay from migrations perfectly (no structural differences).
- **Exit 1**: Failed to provision scratch database, replay failed, target unreachable, or invalid configuration.
- **Exit 2**: Structural differences found (`MISSING`, `EXTRA`, or `CHANGED` objects).

### `dbtools up` & `dbtools push <target>`
- **Exit 0**: Migrations applied successfully (or dry-run printed), and nothing was passed over.
- **Exit 1**: Apply failed partway (cursor marked dirty), connection error, or missing `--yes` on protected targets.
- **Exit 2**: The target holds one or more migration files whose version is below its current
  watermark and which were never applied — files `up`/`push` cannot reach, because both only step
  forward from the watermark. The command prints every ignored path and exits 2 instead of reporting
  a clean run; see [Below-watermark migration files](#below-watermark-migration-files).

### `dbtools down <target>` & `dbtools rollback <target>`
- **Exit 0**: Down migrations executed or soft-revert recorded in ledger.
- **Exit 1**: Missing `.down.sql` file, connection error, or missing `--yes` on protected targets.

---

## Below-watermark migration files

`up` and `push` apply only the files above the target's current watermark (the highest applied
version in the ledger). A migration file whose version is **below** that watermark and which the
ledger does not list as applied can therefore never be applied by any future run: the tool would
print `now at version <watermark> (0 pending)`, exit `0`, and leave the file unapplied forever.

That state is a reported condition, not a silent skip:

- `status` names each such file under its target line, prefixed `ignored:`.
- `status --json` exposes them in the `ignored` field: a list of migration file paths, **always
  present** — `[]` when there are none, never absent and never `null`. `plan --json`, `up --json`
  and `push --json` carry the same field.
- `up` and `push` print every ignored path and exit **`2`** (pending/drift: a human has to decide),
  rather than exiting `0` with a clean-looking report.
- `plan` exits **`2`** as well, so an agent that previews before applying is not told a target with
  permanently unapplied files is safe to apply.

Nothing is repaired automatically — `up` will not apply a below-watermark file out of order. The
remedies are to renumber the file above the watermark, or to revert the newer migrations
(`down`/`rollback`) and re-run `up` so the file becomes pending again.

```console
$ dbtools status local
local       up to date
            1 migration file(s) below the watermark (v20260103000000) that will never be applied:
              ignored: migrations/20260102000000_b.sql
$ dbtools up local
local: now at version 20260103000000 (0 pending)
local: 1 migration file(s) below the current watermark (v20260103000000) were skipped and will never be applied:
local:   migrations/20260102000000_b.sql
local: 1 migration file(s) below the current watermark (v20260103000000) are permanently unapplied (see the paths above) — renumber them above the watermark, or revert the newer migrations and re-run the command
$ echo $?
2
```

---

## Machine JSON Output (`--json`)

Every `dbtools` command supports universal `--json`:

- **stdout**: Contains exclusively machine-readable JSON.
- **stderr**: Diagnostic messages, errors, and warnings.
- **Exit code**: Reflects status (`0`, `1`, or `2`) even when `--json` is enabled.

### JSON contract

- **One document per invocation.** No command emits a second JSON value on
  stdout. A command that has progress/events to stream emits NDJSON — every
  line a complete JSON object — and says so in its docs. `up`, `status`,
  `adopt` are single-document today.
- **Every documented field is always present.** State fields never use
  `omitempty`: `false` and `[]` are emitted explicitly. Absence means the
  field does not exist in this version of the tool, not "false".
  (Exception: `error` — genuinely optional.)
- **Empty lists are `[]`, never `null`.** A nil slice is a bug. (`pending`,
  `ignored`, `drift` are the list fields on the status/plan payloads.)
- **Consumers should validate the shape and fail closed** on any field they
  don't recognise, rather than defaulting silently.

### Example Agent Loop

```bash
# 1. Preview pending changes and check exit code
dbtools plan --json
PLAN_CODE=$?

if [ $PLAN_CODE -eq 0 ]; then
  echo "Target is clean and up to date."
elif [ $PLAN_CODE -eq 2 ]; then
  echo "Pending migrations or drift detected."
  # 2. Inspect pending SQL
  dbtools up --dry-run --json
  # 3. Apply changes with confirmation
  dbtools up --json
else
  echo "Error running dbtools plan"
  exit 1
fi
```

## Container Jobs & Retry Semantics

When running `dbtools` in headless container job orchestrators (Azure Container Apps Jobs, AWS ECS RunTask, Kubernetes Jobs, GCP Cloud Run):

1. **Job Completion Assessment**:
   - Exit `0`: Platform considers job completed successfully.
   - Exit `1`: Platform marks job execution failed.
   - Exit `2`: Inspection detected pending migrations or schema drift (used by audit/gate jobs).

2. **Platform Retry Hazard vs. Dirty Cursor Refusal**:
   - Cloud platforms often allow automatic task retries (e.g. Azure `replica_retry_limit > 0`, Kubernetes `backoffLimit > 0`).
   - If a migration fails midway through execution, `dbtools` flags the migration cursor `dirty=true` in the ledger.
   - On subsequent automatic container retries, `dbtools` inspects the ledger before running any SQL, detects the dirty cursor, and **fails closed immediately with exit code 1**.
   - This ensures retries will not execute out-of-order DDL or replay partial migrations until an operator explicitly repairs the ledger (`dbtools repair <target> <version>:<status> --yes`).

---

## Non-Interactive Mode

When running in automated CI environments or agent shells, `dbtools` respects:
- `DBTOOLS_NO_PROMPT=1`
- `CI=1` / `CI=true`
- Global `--json` flag

In non-interactive mode, commands that perform destructive operations (such as `reset`, `push` to protected target, `down` on protected target, or `rollback`) fail immediately and closed if the required confirmation flag (`--yes`) is not provided, rather than blocking on stdin.
