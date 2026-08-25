# dbtools container lifecycle — project scoping reference

Every `dbtools start`/`stop`/`restart`/`logs` invocation resolves a
**project identity** so two dbtools checkouts on one machine never collide
on a container or volume *name*. This does **not** extend to a fixed
`[container] port` value: if you pin one, it's your responsibility to keep
it unique per machine — two projects both pinning `port = 55432` will have
the second `dbtools start` fail with a Docker port-already-bound error.
Leave `[container] port` unset (the default) to let Docker assign a free
port and avoid this entirely.

## Identity

- Default: first 8 hex chars of SHA-256(absolute path to `dbtools.toml`) —
  automatic, no config needed.
- Override in `dbtools.toml`:

  ```toml
  [project]
  name = "myapp"
  ```

  Must match `^[a-zA-Z0-9][a-zA-Z0-9_.-]*$` (Docker's container-name
  rules) — an invalid name fails fast before any `docker` command runs.

Container name: `dbtools-<engine>-<identity>`. Volume name:
`<container-name>-data`.

## Ports

```toml
[container]
port = 55432
```

Unset (default): `dbtools start` lets Docker assign a free host port.
Set: that exact port is published every time.

## Data persistence

`dbtools stop` removes the container but **keeps its data volume** — the
next `dbtools start` resumes with the same data. Use `dbtools stop
--no-backup` to also delete the volume (today's full-wipe behavior).
`dbtools reset` is unaffected either way — it always drops and recreates
the database via SQL, regardless of volume state, **for the engines it
supports** (currently mssql and postgres only — `dbtools reset` on a MySQL
target errors with "reset does not support engine \"mysql\"", a known,
separate gap from container lifecycle).

## Commands

- `dbtools restart [--timeout] [--no-wait]` — `stop` then `start`, same
  flags as `start`, data survives.
- `dbtools logs [-f]` — streams the container's logs; `-f`/`--follow`
  keeps streaming new output.

## Red flags

- ❌ Assuming the local database URL's port is stable across `stop`/`start`
  when `[container] port` is unset — it's Docker-assigned each time the
  container is recreated. Read it from `.dbtools/local.env`, don't hardcode it.
- ❌ Expecting `dbtools reset` to also reset container-level state (image,
  volume) — it only touches the database's SQL contents.
