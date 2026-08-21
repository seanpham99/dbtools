# Contributing to dbtools

Thank you for your interest in contributing to **dbtools**! We welcome bug reports, feature suggestions, documentation improvements, and code contributions.

Please review this guide before opening an issue or submitting a pull request.

---

## Code of Conduct

We are committed to providing a welcoming, inclusive, and harassment-free environment for all contributors. Please treat others with respect, empathy, and constructive collaboration.

---

## How to Contribute

### 1. Issues First

Before starting work on a non-trivial change, please [open an issue](https://github.com/seanpham99/dbtools/issues/new/choose) to discuss your proposal. This ensures alignment on design and prevents duplicated effort.

- **Bug Reports**: Use the Bug Report issue template with full reproduction steps, OS, Go version, and target database engine.
- **Feature Requests**: Use the Feature Request template detailing the use case, proposed CLI/API behavior, and alternatives considered.

### 2. Fork and Branch

1. Fork the repository on GitHub: `https://github.com/seanpham99/dbtools`
2. Clone your fork locally:
   ```bash
   git clone https://github.com/<your-username>/dbtools.git
   cd dbtools
   ```
3. Create a descriptive feature branch from `main`:
   ```bash
   git checkout -b feat/my-new-feature
   # or
   git checkout -b fix/issue-description
   ```

### 3. Development Workflow

`dbtools` is written in Go 1.25+ and uses standard Go tooling.

- **Format code**:
  ```bash
  gofmt -s -w .
  ```
- **Vet code**:
  ```bash
  go vet ./...
  ```
- **Build**:
  ```bash
  go build ./...
  ```

---

## Testing Guidelines

Every bug fix and feature addition must include accompanying unit or integration tests.

### Running Unit Tests

Unit tests require no external database services:

```bash
go test ./...
```

### Running SQLite Integration Tests

SQLite integration tests run locally against in-memory or temporary database files:

```bash
DBTOOLS_TEST_SQLITE_URL="sqlite:///tmp/dbtools-it.db" go test -tags=integration ./internal/engine/sqliteengine/... -count=1
```

### CI Integration Matrix

GitHub Actions automatically runs integration tests against live MSSQL Server 2022, PostgreSQL 17, and SQLite instances.

> [!IMPORTANT]
> **Secret and Credential Hygiene**: Never hardcode credentials, passwords, or connection strings in test files or documentation. GitGuardian and CI scans actively enforce secret detection.

---

## Submitting Pull Requests

1. **Keep Changes Focused**: Make small, incremental PRs that address a single concern.
2. **Ensure Clean History**: Write clear, descriptive commit messages following the Conventional Commits format (`feat:`, `fix:`, `docs:`, `ci:`, `test:`, `refactor:`).
3. **Verify Locally**: Confirm all tests pass locally before pushing.
4. **Open PR**: Submit your PR targeting `main`. Describe what changes were made and how they were tested.
