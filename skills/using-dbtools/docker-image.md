# dbtools Docker image reference

`ghcr.io/seanpham99/dbtools:<version>` (also tagged `:latest`) ships the
`dbtools` binary on `gcr.io/distroless/static` — CA certificates and
tzdata included (needed for TLS to cloud databases like Azure Database
for PostgreSQL or RDS), no shell. `linux/amd64` and `linux/arm64`.

The image ships the binary only — never a project's `dbtools.toml` or
`migrations/`. Two ways to supply them:

## Mount (default)

```bash
docker run --rm -v "$(pwd)":/workspace \
  -e DBTOOLS_LOCAL_URL \
  ghcr.io/seanpham99/dbtools:0.4.0 up
```

`WORKDIR` inside the image is `/workspace` — `dbtools.toml` and
`migrations_dir` (default `migrations/`) are expected relative to it,
same as running the binary locally from your project root.

## Build `FROM` it

For a private-network job runner (Azure Container Apps job, ECS
RunTask, Kubernetes Job, Cloud Run job) where the database has no public
endpoint and migrations must run from inside the network, teams
typically reuse a single image for the whole job rather than adopting a
second one:

```dockerfile
FROM ghcr.io/seanpham99/dbtools:0.4.0
COPY dbtools.toml ./
COPY migrations/ ./migrations/
```

Then point the job's entrypoint/command at the same `dbtools` binary
(`up`, `plan --json`, `doctor --json`, etc.) with `DATABASE_URL`/
`DBTOOLS_*_URL` injected the same way the job already injects secrets
(Key Vault → container secret → env var, AWS Secrets Manager → task
definition secret → env var — `url_env` composes with either
unchanged).

## Red flags

- ❌ Baking `dbtools.toml`/`migrations/` into a *published, shared*
  image — the mount pattern exists so one dbtools image serves every
  project; only a project's own job/app image should bake its own files
  in via the build-`FROM` pattern.
- ❌ Expecting a shell inside the image for debugging — `distroless/static`
  has none. Use `docker cp`/multi-stage builds if you need one during
  development.
