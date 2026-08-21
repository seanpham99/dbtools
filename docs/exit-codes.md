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
- **Exit 2**: One or more targets have pending unapplied migrations, schema drift, or dirty ledger cursors.

### `dbtools verify <target>`
- **Exit 0**: Database matches the migration ledger cleanly (all applied objects exist and hashes match).
- **Exit 1**: Failed to query database, ledger missing without `--init-ledger`, or database error.
- **Exit 2**: Drift detected (objects created by an applied migration are missing or content hash mismatch).

### `dbtools up` & `dbtools push <target>`
- **Exit 0**: Migrations applied successfully (or dry-run printed).
- **Exit 1**: Apply failed partway (cursor marked dirty), connection error, or missing `--yes` on protected targets.

### `dbtools down <target>` & `dbtools rollback <target>`
- **Exit 0**: Down migrations executed or soft-revert recorded in ledger.
- **Exit 1**: Missing `.down.sql` file, connection error, or missing `--yes` on protected targets.

---

## Machine JSON Output (`--json`)

Every `dbtools` command supports universal `--json`:

- **stdout**: Contains exclusively machine-readable JSON.
- **stderr**: Diagnostic messages, errors, and warnings.
- **Exit code**: Reflects status (`0`, `1`, or `2`) even when `--json` is enabled.

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

---

## Non-Interactive Mode

When running in automated CI environments or agent shells, `dbtools` respects:
- `DBTOOLS_NO_PROMPT=1`
- `CI=1` / `CI=true`
- Global `--json` flag

In non-interactive mode, commands that perform destructive operations (such as `reset`, `push` to protected target, `down` on protected target, or `rollback`) fail immediately and closed if the required confirmation flag (`--yes`) is not provided, rather than blocking on stdin.
