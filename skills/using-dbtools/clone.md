# dbtools clone — masking reference

`dbtools clone <source> <dest> --yes` copies **data only** (schema must already
match on both targets — same `migrations_dir`, same engine). It is as
destructive to `dest` as `reset`: no override for a protected `dest`, unlike
`push`'s softer `--yes` rule.

## Masking

Masking is **on by default**. `--no-mask` opts out and is a documented PII
risk — never suggest it for a dest that isn't fully trusted.

Built-in default-deny list (case-insensitive exact column name match,
applies even with no config):

| Column name | Strategy | Behavior |
|---|---|---|
| `email` | `email` | Replaced with a deterministic synthetic address (`user1@example.invalid`, `user2@...`), unique per row so unique constraints still hold |
| `phone` | `redact` | Replaced with `[REDACTED]` |
| `ssn` | `redact` | Replaced with `[REDACTED]` |
| `password` | `redact` | Replaced with `[REDACTED]` |

Other strategy: `hash` — deterministic 12-hex-char SHA-256 prefix (same
input always maps to same output; not reversible). Useful for columns
referenced elsewhere that need to stay joinable without being exposed.

`NULL` values always pass through as `NULL` regardless of strategy — masking
never invents data for a column that was genuinely absent.

### Overriding the plan

Add a `[clone.mask]` table to `dbtools.toml` to mask additional columns or
change a built-in's strategy; an explicit entry always wins over the
built-in default:

```toml
[clone.mask]
ssn = "hash"          # override built-in redact -> hash
internal_notes = "redact"   # not in the built-in list, needs an explicit entry
```

An unrecognized strategy name is a no-op (value passed through unchanged) —
double-check spelling against `redact` / `email` / `hash`.

## Other flags

- `--limit N` — copy at most N rows per table (0 = no limit).
- `--where "SQL"` — filter appended to every table's `SELECT`, **trusted and
  unsanitized**. Never build this from untrusted input.

## Red flags

- ❌ `--no-mask` against a dest anyone outside the team can reach.
- ❌ Assuming a column not named exactly `email`/`phone`/`ssn`/`password` is
  safe by default — add it to `[clone.mask]` explicitly.
- ❌ Cloning into a dest whose schema hasn't been migrated to match source —
  clone does not create or alter tables.
