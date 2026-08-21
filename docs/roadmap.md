# dbtools roadmap

Status: **living document** — updated as features land. Community input welcome via issues.

## Direction

dbtools is a database migration and ops tool for MSSQL, Postgres, and SQLite, built for
engineering teams and AI coding agents. Today it is a free, self-hosted CLI. The
architecture deliberately keeps the door open to hosted/enterprise offerings later —
every command is CLI-first with structured output, so a future service can wrap the
same core.

## Design constraints

1. **CLI-first, structured output everywhere.** Every command emits `--json`;
   a stable exit-code contract distinguishes clean / drift / error. Machine output on
   stdout only; human logs on stderr.
2. **Read-only by default.** `verify`, `plan`, `status`, `doctor` never write.
   Destructive operations require explicit flags and human-in-the-loop confirmation on
   production targets.
3. **The ledger is truth.** Every applied migration records its status, content hash,
   and timestamp. No command writes the ledger without recording the file it executed.

## Phases

| Phase | Feature | Notes |
|-------|---------|-------|
| v0.1.2 ✅ | `plan` preview | Read-only preview of pending migrations + ledger drift. Agent/CI-friendly: exit 0 = safe to apply. |
| v0.2 | **Rollback & down migrations** | Biggest functional gap: down-migration support + safe ledger-level rollback, with production gates. See design below. |
| v0.2 | **Agent ergonomics** | Universal `--json`, `--dry-run`, non-interactive mode, dirty-ledger refusal. See design below. |
| v0.3 | **`doctor`** | Read-only health/security check: connectivity, version sync, ledger integrity, drift summary, basic security flags. One-call parseable health. |
| v0.3/4 | **Clone prod→dev** | Schema + data clone with config-driven masking. Masking on by default; raw copy requires explicit opt-out. |
| v0.4 | **Backup** | Table-stakes backup/restore. |
| launch | — | Public launch (announcements, directories) happens only after the v0.2/v0.3 features above. |

## Rollback & down migrations

- Support `.down.sql` files (golang-migrate already parses them; dbtools currently
  ignores them).
- `dbtools down <target> [N]` — applies down files in reverse, each recorded in the
  ledger with its content hash.
- `dbtools rollback <target>` — **ledger-level only** soft-revert (marks versions
  reverted). Never data-destroying; the safe verb for production.
- Production gate: `down` requires `--preview --yes` and prints exactly what will be
  destroyed. Local targets unrestricted.
- Rationale: a down migration that drops a column destroys data irrecoverably; several
  production tools ban automatic down-migrations for exactly this reason. The two-verb
  split — destructive `down` vs ledger-only `rollback` — is the answer.

## Agent ergonomics

Adopted patterns (each proven by an existing tool):

1. **Exit-code contract distinguishes drift from error** — Terraform `-detailed-exitcode`
   style: `0` clean, `1` error, `2` drift/changes. Agents branch on it.
2. **`--dry-run` as the pre-apply gate** — `up --dry-run` prints the SQL and exits 0;
   real apply requires `--yes`. Never prompt in non-TTY.
3. **Structured JSON everywhere** — one `--json` flag on all commands, stable field
   names, machine output on stdout only.
4. **Non-interactive mode** — `DBTOOLS_NO_PROMPT=1` (or non-TTY): never block on stdin;
   destructive ops fail closed.
5. **Dirty-ledger refusal** — `up` auto-refuses when the ledger is dirty; verify
   surfaces content-hash drift as an exit-2-class state, not silent success.
6. **Agent skill surface** — ship a `SKILL.md` + `llms.txt` command inventory so agents
   self-restrict to safe commands (pattern: Atlas Agent Skills).

## Clone (prod→dev)

- `dbtools clone <source> <dest> [--mask|--no-mask]`
- Masking is config-driven: column masks (email, phone, names), default-deny on
  sensitive columns, optional subsetting (row-count / WHERE).
- Raw copy is explicit (`--no-mask`) and documented as a PII risk. Masking is the default.

## License

MIT. A future hosted/enterprise pivot may revisit licensing (a community-trust event,
not a silent change); today the permissive license maximizes adoption.

## Open questions (deferred)

- Backup scope (v0.4): full vs incremental, engine support, restore test strategy.
- `doctor` exact check list — spec at v0.3 start.
- Exact exit-code values — written up in `docs/exit-codes.md` with v0.2.
