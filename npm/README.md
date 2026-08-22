# dbtools-cli

Fast schema migrations and dev databases for MSSQL, PostgreSQL, and SQLite.

This is the npm distribution of [`dbtools`](https://github.com/seanpham99/dbtools) — a thin wrapper that downloads the official Go binary for your platform. Features: version-sync migrations, SHA-256 ledger, read-only drift verification, rollback/down, TypeScript + Pydantic type generation, and agent-friendly exit codes / `--json` output.

## Usage with npx (zero installation)

```bash
# Run without installing
npx dbtools-cli status

# Apply local migrations
npx dbtools-cli up

# Preview migrations and drift with exit-code contract
npx dbtools-cli plan --json
```

## Global Installation

```bash
npm install -g dbtools-cli

# Now run directly
dbtools status
dbtools up
dbtools verify local
```

## Supported Platforms

- macOS (x86_64 and arm64 Apple Silicon)
- Linux (x86_64 and arm64)
- Windows (x86_64)

The npm package automatically fetches and caches the matching native `dbtools` binary from official GitHub Releases.
