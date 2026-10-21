# pgx as an alternative local-database provider

**Status:** accepted, implemented
**Date:** 2026-10-21
**Relates to:** ADR 003 (version parity), the `internal/container` lifecycle

## Context

`dbtools start` provisions the tool-owned local database in a Docker
container. That makes Docker a hard dependency of the local dev loop:
a contributor without a running Docker daemon cannot `dbtools start`,
and on a host where the daemon is slow to respond, every start pays a
container-boot cost before the first migration can run.

[pgx](https://github.com/pgrundev/pgx) is a small, self-contained
PostgreSQL-compatible runtime. It speaks the PostgreSQL wire protocol,
so a pgx cluster is reachable by dbtools' existing postgres engine with
no engine-level changes at all.

Measured against the real thing (pgx v0.1.0-beta.1, PostgreSQL 18.6):

| Operation | Docker | pgx |
|---|---|---|
| `dbtools diff` with a scratch database | 3.10 / 3.13 / 3.42 s | 0.315 / 0.292 / 0.290 s |
| create / clone a database | container-bound | 44 ms / 39 ms |
| cold boot | 1.19–1.36 s (433 MB image) | ~0.3 s |

dbtools' native byte-exact `diff` reported **zero findings** in both
directions between a pgx cluster and a real `postgres:18-alpine`, and
`information_schema` / `pg_catalog` introspection was byte-identical.
The wire protocol compatibility is not approximate.

Two facts constrain the design:

- **pgx is AGPL-3.0; dbtools is MIT.** pgx's licence is incompatible with
  distributing it inside an MIT-licensed binary.
- **pgx is PostgreSQL 18.6 only.** PostgreSQL 18 exposes `NOT NULL`
  constraints as named `pg_constraint` rows; PostgreSQL 17 does not. The
  introspection differs by 16 lines between the two majors, so pgx is
  not a drop-in for a PostgreSQL 16 or 17 local database.

## Decisions

| # | Decision | Rationale |
|---|---|---|
| 1 | pgx is an **external executable** dbtools invokes as a subprocess. It is not linked, vendored, bundled in the npm package, or copied into the Docker image | Keeps AGPL-3.0 out of the MIT-licensed distribution. This is the same arrangement dbtools already has with `docker`, `psql`, and `pg_dump` — tools it calls, never ships |
| 2 | The provider is **opt-in** via `[local] provider = "pgx"`. Docker stays the default | Every existing user keeps today's behaviour. Docker still works for every engine; pgx serves postgres only |
| 3 | The provider boundary is a **single interface** (`internal/localdb.Provider`) covering start, stop, restart, logs, the reset maintenance URL, and prune | The commands must not know which provider they are driving. A per-command `if pgx` would spread the concept across every file that touches the local database |
| 4 | **One cluster per project**, under `~/.dbtools/pgx/<engine>/<project-id>`, with the socket directory kept separate | Matches the docker provider's per-project isolation, and the separate socket directory is required — pgx needs a short path for the socket file |
| 5 | Every pgx invocation carries `PGX_HOME` and `PGX_RUN_DIR` | The cluster a command touches is then always the one the project owns, never one inherited from the ambient environment |
| 6 | The provider falls back to **docker when no config is readable** | `stop` and `logs` must keep working in a directory that has no `dbtools.toml` |
| 7 | pgx URLs get **`sslmode=disable`** | pgx speaks no TLS, and dbtools' postgres engine requires TLS by default. Without this every connection fails |
| 8 | Reset's maintenance URL comes from the **cluster**, never from user configuration, and the local target's port must match the cluster's port | Preserves the docker provider's guarantee that reset can never be aimed at a real server. pgx generates a random password per cluster and reports it only through its own URL command, so the cluster is the only safe source of the maintenance URL |
| 9 | The pgx provider serves the **postgres engine only**, and says so | pgx speaks the PostgreSQL wire protocol. It cannot serve mssql or mysql |
| 10 | A missing pgx binary is an **error for start, a skip for prune** | Start cannot work without it. Prune on a host that never used pgx has nothing to clean up and must not fail |
| 11 | `prune` **stops** a dead project's pgx cluster and keeps its data, mirroring how the docker provider prunes a container but keeps its volume; `--volumes` removes the cluster | A pgx cluster holds its runtime and its data together and cannot be split the way a container and a volume can. The flag keeps the destructive option explicit |
| 12 | A stopped cluster is **marked** so a later prune does not report it again | The docker provider removes the container, so the leak is gone. pgx keeps the cluster directory, which would otherwise be reported on every prune |

## Consequences

- `dbtools start`/`stop`/`restart`/`logs`/`reset`/`prune` work against a
  pgx cluster with no Docker and no engine-level changes.
- Switching a project between providers is a config change, not a
  migration: the database name (`dbtools_local`) is the same, so the
  schema is portable.
- A pgx local database is **PostgreSQL 18**. A project that needs 16 or
  17 keeps the docker provider. dbtools does not paper over this.
- The `squash` path still needs a host PostgreSQL 18 installation: the
  pgx distribution ships the runtime, not `pg_dump`.
- pgx is a young beta. The provider is opt-in for that reason as much as
  for the licensing one.

## Explicitly refused

- **Bundling or vendoring pgx.** Would put AGPL-3.0 code inside an
  MIT-licensed binary.
- **A pgx-backed scratch database for `diff`.** The scratch path is a
  separate concern from the local dev loop and has its own version-parity
  rules under ADR 003. Not attempted here.
- **Supporting pgx for mssql or mysql.** pgx speaks the PostgreSQL wire
  protocol; there is nothing to support.
- **Claiming pgx as a general PostgreSQL replacement.** It is PostgreSQL
  18.6, and the version difference is visible in introspection.
