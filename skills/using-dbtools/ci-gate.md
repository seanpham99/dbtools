# dbtools exit codes — CI/agent automation contract

Terraform `-detailed-exitcode` style. Every command's exit code is one of
three values — never treat non-zero as a monolithic "failure":

| Code | Meaning |
|:---:|---|
| `0` | Success / clean — no pending changes, no drift |
| `1` | Fatal error — network, config, syntax, missing `--yes` on protected target |
| `2` | Inspection succeeded but found pending migrations / drift / dirty ledger |

Per-command specifics (`plan`, `verify`, `up`/`push`, `down`/`rollback`):
`docs/exit-codes.md`.

## Agent loop pattern

```bash
dbtools plan --json
PLAN_CODE=$?

if [ $PLAN_CODE -eq 0 ]; then
  echo "clean, nothing to do"
elif [ $PLAN_CODE -eq 2 ]; then
  dbtools up --dry-run --json   # inspect before applying
  dbtools up --json
else
  echo "error running dbtools plan" >&2
  exit 1
fi
```

`--json` puts machine-readable output on stdout and diagnostics on stderr;
the exit code still reflects `0`/`1`/`2` even with `--json` set.

## Non-interactive mode

Set `DBTOOLS_NO_PROMPT=1` or `CI=1`/`CI=true` (or pass `--json`, which
implies non-interactive). In this mode, destructive operations without
`--yes` fail closed immediately instead of blocking on stdin — never assume
a script can rely on an interactive confirmation prompt in CI.

## Red flag

`if dbtools plan; then ...` — collapses `2` into the same branch as `1`.
Always capture `$?` and switch on it explicitly.
