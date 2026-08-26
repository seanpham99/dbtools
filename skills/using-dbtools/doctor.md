# dbtools doctor — checks reference

`dbtools doctor [target] [--json]` is strictly **read-only**: never modifies
the database, ledger, or state. Runs 6 independent checks per target.

| Exit | Meaning |
|:---:|---|
| `0` | HEALTHY — all checks `[OK]`, `[WARN]`, or `[SKIP]` |
| `1` | ERROR — unreachable target, bad config, unreadable migrations dir |
| `2` | ISSUES — drift, hash mismatch, pending migrations, or dirty ledger |

## Checks

1. **connectivity** — resolves `url_env`, confirms engine registered, pings.
   Fail (`1`): connection refused/bad creds/timeout/unknown target.
2. **ledger-integrity** — SHA-256 of each applied migration file vs. the
   hash recorded when it was applied. Skip: no ledger table exists. Warn:
   ledger empty. Fail (`2`): hash mismatch (file edited post-apply) or corrupted history.
3. **version-sync** — live DB version vs. `.up.sql` files on disk. Fail
   (`2`): pending migrations exist. (Appends `(no dbtools ledger)` when no ledger exists).
4. **drift-summary** — live schema objects vs. what applied migrations
   created. In ledger-free mode: walks migration files directly checking object presence.
   Fail (`2`): an applied object was dropped/altered out-of-band.
5. **dirty-ledger** — Skip: no ledger table and no migration cursor. Fail (`2`): target's `dirty` flag is true (a previous
   migration failed partway through).
6. **security-flags** — Warn: plaintext URL in config instead of `url_env`,
   or missing migrations dir.

## Ledger-Free Mode

When pointed at a database with no `dbtools_migration_history` table (e.g. managed by Flyway, Knex, or raw migrations), read-only commands adapt automatically:

- **`doctor`**: `ledger-integrity` and unmigrated `dirty-ledger` report `[SKIP]` (exit `0` healthy). `drift-summary` checks object presence directly.
- **`verify`**: Walks migration files directly checking live object presence (Status `OK` / `DRIFT`, note: no content-hash check possible) instead of hard-failing with exit 1. Pass `--init-ledger` to create and backfill.
- **`plan`**: Sets `ledger_skipped: true` in JSON; clean object presence produces zero drift entries.
- **`status`**: Emits `no_ledger: true` in JSON and a text note prompting `dbtools adopt`.

**On-ramps:**
- Run `dbtools adopt <target> --yes` to import existing history from another tool.
- Pass `dbtools verify <target> --init-ledger` to initialize and backfill the dbtools ledger from the migrate cursor.

Full check reference with sample output: `docs/doctor-checks.md`.

## Common mistake

Treating exit `2` as a passing run in a CI script — it means issues were
*detected*, not that the command errored. Branch on `0`/`1`/`2` separately;
see `ci-gate.md` for the pattern.

