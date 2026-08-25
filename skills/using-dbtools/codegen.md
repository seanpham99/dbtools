# dbtools generate — schema codegen reference

`dbtools generate [target] [flags]` introspects a target's **live** database
schema (so migrations must be applied first) and emits typed models.

| Flag | Effect |
|---|---|
| `--lang python` (default) | Pydantic `BaseModel` classes |
| `--lang ts` | TypeScript interfaces |
| `--zod` | With `--lang ts`, also emit zod schemas per interface. Requires `--lang ts` — errors otherwise |
| `--out PATH` | Output file (default: `[generate] out` in `dbtools.toml`, else `db_models.py`) |
| `--yes` | Required when `target` is `prod` |
| `--check` | Don't write. Exit non-zero if the file on disk is stale vs. what would be generated |

## CI drift gate

Use `--check` in CI to catch a migration that shipped without its model
file being regenerated:

```bash
dbtools generate local --check
```

On staleness this prints the exact command to refresh (including the
`--lang`/`--zod` flags it detected from the existing file), so you don't
need to remember your own invocation.

## Common mistake

Running `generate` before applying pending migrations — it reads the
**live** schema, not the `.sql` files, so newly written migrations that
haven't been applied yet won't appear in the output. Run `dbtools up` (or
`dbtools plan` to confirm nothing's pending) first.
