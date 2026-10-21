# dbtools pgx provider — no-Docker local database

The pgx provider runs the tool-owned local database in an external
[pgx](https://github.com/pgrundev/pgx) runtime instead of a Docker
container. It serves the **postgres** engine only.

Enable it in `dbtools.toml`:

```toml
[local]
provider = "pgx"
```

Everything else stays the same: `dbtools start` provisions the database,
`dbtools up` migrates it, `dbtools reset` drops and recreates it, and the
resolved connection URL lands in `.dbtools/local.env` exactly as it does
for the docker provider.

## What pgx is, and what it is not

pgx is a **separate program** that dbtools invokes as a subprocess. It is
not bundled with dbtools, not linked into it, and not redistributed by it.
Install it yourself:

```bash
curl -fsSL https://pgx.dev/install | sh
```

dbtools is MIT-licensed; pgx is AGPL-3.0. Keeping pgx an external
executable is what keeps the AGPL code out of the dbtools distribution —
the same arrangement dbtools already has with `docker`, `psql`, and
`pg_dump`.

## Project scoping

Each project gets its own pgx cluster, so two checkouts on one machine
never share a database:

```
~/.dbtools/pgx/<engine>/<project-id>/     the cluster (its data lives here)
~/.dbtools/pgx/run/<engine>/<project-id>/ the socket directory
```

`<project-id>` is the same identity the docker provider uses — the
`[project] name` from `dbtools.toml`, or the first 8 hex chars of
SHA-256(absolute path to `dbtools.toml`).

The socket directory is separate from the cluster because pgx needs a
short path for the socket file.

Every pgx invocation carries `PGX_HOME` and `PGX_RUN_DIR`, so the cluster
a command touches is always the one this project owns.

## Ports

pgx assigns each cluster a free port at creation, and the port is stable
for that cluster afterwards.

`[local] port` applies to the pgx provider as well: it pins the port the
cluster is **created** with. It has no effect on a cluster that already
exists, because pgx records the port when the cluster is initialised — so
changing the setting later does not move an existing cluster. Two projects
that pin the same port will collide, the same way they do with the docker
provider. The old `[container] port` key is deprecated, still honored, and
prints a notice on `start`/`restart`.

Read the port from `.dbtools/local.env`; never hardcode it.

## Postgres major

The pgx runtime serves **one** PostgreSQL major — 18.6 for the current
release, reported by the binary itself. The local database's major is
therefore a property of the provider unless the project pins it:

```toml
[local]
postgres_major = "18"
```

With `postgres_major` unset the provider's default applies (pgx 18,
docker 17). Switching providers then changes the local database's major,
and `dbtools start` says so when the other provider's local database still
exists on this machine. Pinning `postgres_major` makes that impossible:

- pgx + a pin of 18 → works.
- pgx + a pin of any other major → `dbtools start` fails, naming both
  majors, because the runtime cannot serve the pin. It never silently
  creates a cluster on its own major instead.
- docker + a pin → builds `postgres:<major>-alpine`, subject to the same
  data-directory rule as any major change.

`dbtools doctor` reports the local database's major next to the target's.

## Schema comparisons

`dbtools diff` uses a pgx scratch cluster when the project selected
`provider = "pgx"` **and** the target runs the same major the runtime
serves. Any other target keeps the container scratch, because a scratch
database rendered by a different major reports the difference as drift.
When the comparison cannot use pgx, diff says why in its notes.

`squash` always uses the container scratch: it dumps the replay database
with the engine's own tool, run inside that container so the tool's version
matches the server's.

Each comparison gets its own scratch cluster, named `diff-<pid>-<nano>` and
removed when the comparison ends. A dbtools process killed mid-run leaves
that cluster behind, exactly as a killed diff leaves its `--rm` container
running; `dbtools prune` reaps it once the project directory it recorded is
gone.

## Data persistence

`dbtools stop` stops the runtime and keeps the cluster's data, so the
next `dbtools start` resumes with the same database. `dbtools stop
--no-backup` deletes the cluster directory, which destroys the data.

## Commands

- `dbtools start` — creates the cluster if it is missing, starts it,
  creates the `dbtools_local` database, and writes the URL to
  `.dbtools/local.env`.
- `dbtools stop` — stops the runtime; the data survives.
- `dbtools restart` — stop then start; the data survives.
- `dbtools logs [-f]` — streams the cluster's server log.
- `dbtools reset --yes` — drops and recreates the database through the
  cluster's own `postgres` maintenance database, then replays migrations
  and runs `seed.sql`.
- `dbtools prune` — stops (or, with `--volumes`, removes) pgx clusters
  whose owning `dbtools.toml` is gone. Without `--volumes` the cluster's
  data is kept, the way a pruned container's volume is.

## Reset's safety property

Reset drops a database, so it must never be aimed at a real server. The
pgx provider keeps the docker provider's guarantee:

- the maintenance URL comes from the **cluster**, never from user
  configuration — pgx generates a random password per cluster and reports
  it only through its own URL command;
- the local target's port must match the cluster's port, which proves the
  target really is this project's database;
- a non-loopback host is refused outright.

## Red flags

- ❌ Assuming pgx is a general PostgreSQL replacement. The current release
  is **PostgreSQL 18.6 only**. PostgreSQL 16 and 17 differ from 18 in
  their introspection (18 exposes `NOT NULL` constraints as named
  `pg_constraint` rows), so a pgx local database is not a drop-in for
  those.
- ❌ Expecting TLS. pgx speaks the PostgreSQL wire protocol without TLS,
  so dbtools adds `sslmode=disable` to the URL it generates. dbtools'
  postgres engine otherwise requires TLS by default.
- ❌ Expecting `psql` or `pg_dump` to come with pgx. The pgx distribution
  ships the runtime only; the `squash` path still needs a host PostgreSQL
  18 installation for those client tools.
- ❌ Assuming the port is Docker-assigned and changes every time. A pgx
  cluster keeps the port it was created with.
- ❌ Running `dbtools reset` against a target that is not the local one.
  Reset is scoped to the tool-owned local database.
