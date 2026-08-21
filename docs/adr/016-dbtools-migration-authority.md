# ADR 016: dbtools as Migration Authority and Local Dev-Loop

## Status
Accepted

## Context
Engineering teams and autonomous AI agents working across polyglot microservices, ETL pipelines, and reporting backends frequently encounter database schema synchronization issues:
1. **Ad-hoc Migration Execution**: Applying migrations directly from developer laptops or fragmented scripts results in inconsistent versions and untracked database mutations.
2. **Secret Exposure**: Connection strings and database credentials often leak into configuration files or shell histories.
3. **Engine Dialect Disparities**: Different database management systems (MSSQL Server, PostgreSQL, SQLite) require specialized driver configurations, transaction semantics, and session management.
4. **Dev-Loop Friction**: Spinning up local database containers, resetting schema state, and running test fixtures requires tedious manual orchestration.

## Decision
We create `dbtools`, a standalone Go CLI acting as the sole migration authority and developer dev-loop coordinator for relational database engines.

### Key Architectural Tenets:
1. **Target-Based Environment Abstraction**: Targets (e.g. `local`, `staging`, `prod`) are declared in `dbtools.toml` and bound exclusively to environment variable names (`url_env`). Connection strings and credentials are never stored in repository configuration.
2. **Version-Sync Guarantee**: The `up` and `push` commands ensure that applied database migrations exactly mirror the repository's local `.sql` migration files up to the highest applied version.
3. **Protected Targets**: Remote and production targets are marked with `protected = true`, causing destructive commands (`up`, `reset`) to fail fast unless invoked with explicit confirmation through `push <target> --yes`.
4. **Ephemeral Dev-Loop**: `dbtools` owns container provisioning (`start`, `stop`) and atomic database recreation (`reset` + `seed.sql`) for rapid local development.
5. **No Domain Knowledge**: `dbtools` remains strictly a tooling component without business domain concepts.

## Consequences
### Positive
- Unified migration lifecycle across MSSQL, PostgreSQL, and SQLite.
- AI agents and developers operate through consistent CLI commands (`dbtools up`, `dbtools push`, `dbtools status`).
- Zero plaintext credentials stored in version control.
- Fast local schema iteration via ephemeral containers and automated seeding.

### Negative
- Requires maintaining Go driver wrappers and engine abstractions for each supported database dialect.
- Multi-step remote deployments require environment variable provisioning in execution contexts.
