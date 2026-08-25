# dbtools doctor — checks reference

`dbtools doctor [target] [--json]` is strictly **read-only**: never modifies
the database, ledger, or state. Runs 6 independent checks per target.

| Exit | Meaning |
|:---:|---|
| `0` | HEALTHY — all checks `[OK]`/`[WARN]` |
| `1` | ERROR — unreachable target, bad config, unreadable migrations dir |
| `2` | ISSUES — drift, hash mismatch, pending migrations, or dirty ledger |

## Checks

1. **connectivity** — resolves `url_env`, confirms engine registered, pings.
   Fail (`1`): connection refused/bad creds/timeout/unknown target.
2. **ledger-integrity** — SHA-256 of each applied migration file vs. the
   hash recorded when it was applied. Warn: no ledger table yet. Fail
   (`2`): hash mismatch (file edited post-apply) or corrupted history.
3. **version-sync** — live DB version vs. `.up.sql` files on disk. Fail
   (`2`): pending migrations exist.
4. **drift-summary** — live schema objects vs. what applied migrations
   created. Fail (`2`): an applied object was dropped/altered out-of-band.
5. **dirty-ledger** — Fail (`2`): target's `dirty` flag is true (a previous
   migration failed partway through).
6. **security-flags** — Warn: plaintext URL in config instead of `url_env`,
   or missing migrations dir.

Full check reference with sample output: `docs/doctor-checks.md`.

## Common mistake

Treating exit `2` as a passing run in a CI script — it means issues were
*detected*, not that the command errored. Branch on `0`/`1`/`2` separately;
see `ci-gate.md` for the pattern.
