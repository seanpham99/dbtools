# Local database configuration and scratch providers

**Status:** accepted
**Date:** 2026-10-21
**Supersedes:** ADR 004 decision 4b (the `[container] port` key name)
**Extends:** ADR 003 decision 5 (version parity)

## Context

Three problems accumulated around the local database and the scratch
databases `diff`/`squash` provision:

- **#122** — the only port knob in `dbtools.toml` was `[container] port`,
  a section named after one provider that configures both. Its two
  providers also apply it at different moments: docker publishes the
  pinned port whenever it creates the container, pgx records it once when
  the cluster is initialised.
- **#123** — the local database's PostgreSQL major was a side effect of
  the provider: `postgres:17-alpine` for docker, the runtime's 18.6 for
  pgx. A provider switch therefore changed the major with nothing in the
  output saying so, and a schema developed against one major is rendered
  differently by the other.
- **#121** — `diff` and `squash` provisioned their scratch database in an
  ephemeral container, so a project that had opted into pgx still needed
  Docker. Measured against a live pgx runtime, the same replay-and-compare
  work ran roughly 11x faster in a pgx cluster.

## Decisions

| # | Decision | Rationale |
|---|---|---|
| 1 | **One `[local]` section owns the local database**: `provider`, `port`, `postgres_major`. `[container] port` is deprecated, still honored, and named by the deprecation notice that `start`/`restart` print | The section now describes the local database instead of one provider. The legacy key keeps working so no existing `dbtools.toml` silently loses its pinned port, and the notice appears only where the value is used, never on every command |
| 2 | The local major is **explicit and provider-independent**: `[local] postgres_major`, defaulting to the provider's own major (docker 17, pgx 18) | The major had been a silent provider side effect. Pinning it is the only way a provider switch cannot change it |
| 3 | A pin the provider cannot serve is a **hard error naming both majors**, never a substitution. A pin outside the tested window (15-18) is allowed and warned about | The pgx runtime serves one major. Serving its own while the configuration says another is exactly the silent change decision 2 exists to prevent. An out-of-window pin can legitimately mirror a legacy target, so refusing it would leave that project unable to compare schemas at all |
| 4 | The local major is a **data-format boundary**: the docker data volume records the image that initialised it, and a start that would open it under a different image fails with the remedy (`dbtools stop --no-backup`, then `dbtools start`) | A PostgreSQL data directory belongs to the major that wrote it. PostgreSQL 18 additionally refuses to initialise a volume mounted at the pre-18 path. Both failures arrive as server errors that never name the remedy |
| 5 | `dbtools doctor` reports the local database's major next to the target's — the **observed** major (data volume image, or the pgx runtime), not the configured one — as a **warning that never fails the target's report** | The comparison is advisory — the local database is a development convenience — so it must not make an unrelated target unhealthy. Parity in the ADR 003 sense stays what it means there: scratch versus target |
| 6 | **`internal/scratchdb` gets the same provider seam the local database has**: a `Provider` interface with a container implementation and a pgx implementation | A per-command `if pgx` would spread the concept across every caller; ADR 004 decision 3's reasoning applies unchanged |
| 7 | The scratch provider is chosen by **version parity and the project's opt-in**: pgx only when the project selected `provider = "pgx"`, the target is postgres, its major is known, and it equals the major the installed runtime serves. Everything else keeps the container, whose image is pinned to the target's major | This preserves ADR 003 decision 5 by construction rather than by care: a scratch rendered by another major reports the difference as drift. Opt-in keeps a beta runtime out of projects that never asked for it |
| 8 | `squash` **keeps the container scratch**, and refuses to continue if a container-less scratch ever reaches it for an engine whose image ships the dump tool | `squash` dumps the replay database with the engine's own tool, run inside the scratch container so the tool's version matches the server's (ADR 003 decision 7). The pgx distribution ships no `pg_dump` |
| 9 | The pgx scratch cluster is **ephemeral and has an identity of its own** (`diff-<pid>-<nanoseconds>`), initialised with ephemeral durability, recorded with an owning project, and destroyed by the provider's own teardown | Per-invocation isolation is cheap (init + start + create ≈ 0.4 s, against ≈ 3 s for a container), and a distinct identity means a scratch teardown can never remove a project's local database. The owner record lets `dbtools prune` judge a cluster whose process died |

## Release impact

The change lands as a `feat:` (minor). It removes no capability:
`[container] port` still works. Its removal condition is the next major
release, at which point the deprecated key is deleted and any project still
carrying it must move to `[local] port`.

## Follow-up

`squash` on a pgx-only host stays scoped as: run the version-matched
`pg_dump` in a `postgres:18-alpine` container that holds only the tool,
against the pgx scratch cluster. The blocker is not that pgx ships no
`pg_dump`; it is that squash's dump tool and its scratch server currently
share one container, and that coupling is what the follow-up unwinds.
