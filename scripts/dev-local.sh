#!/usr/bin/env bash
# One-command dev/verify entrypoint for dbtools itself (this repo builds a CLI,
# not a service — there is no server to boot, so "dev-local" means build +
# verify against a real SQLite database, no Docker required).
#
# Usage:
#   scripts/dev-local.sh build     # go build ./...
#   scripts/dev-local.sh lint      # gofmt -l, go vet, golangci-lint
#   scripts/dev-local.sh test      # go test ./... (unit tests only)
#   scripts/dev-local.sh smoke     # build the binary, run it through init/new/up/status/verify/reset on a temp sqlite db
#   scripts/dev-local.sh all       # build + lint + test + smoke (default)
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."

cmd_build() {
  echo "==> go build ./..."
  go build ./...
}

cmd_lint() {
  echo "==> gofmt -l"
  local unformatted
  unformatted=$(gofmt -l . | grep -v '^$' || true)
  if [ -n "$unformatted" ]; then
    echo "gofmt found unformatted files (run: gofmt -s -w .):"
    echo "$unformatted"
    exit 1
  fi
  echo "==> go vet ./..."
  go vet ./...
  if command -v golangci-lint >/dev/null 2>&1; then
    echo "==> golangci-lint run"
    golangci-lint run
  else
    echo "golangci-lint not installed locally — skipping (CI still runs it)."
  fi
}

cmd_test() {
  echo "==> go test ./... (unit tests, no external DB needed)"
  go test ./...
}

# Black-box smoke test of the actual CLI against a real sqlite:// database.
# Exercises the golden path an agent or CI would run: init -> new -> up ->
# status -> verify -> reset, and checks the Terraform-style exit codes.
cmd_smoke() {
  cmd_build
  local bin="$PWD/dbtools-smoke"
  go build -o "$bin" .

  local tmpdir
  tmpdir=$(mktemp -d)
  trap "rm -rf '$tmpdir' '$bin'" EXIT
  pushd "$tmpdir" >/dev/null

  echo "==> dbtools init"
  "$bin" init

  cat > dbtools.toml <<'EOF'
migrations_dir = "migrations"

[targets.local]
url_env = "DBTOOLS_LOCAL_URL"
engine = "sqlite"
EOF
  export DBTOOLS_LOCAL_URL="sqlite://dev.db"

  echo "==> dbtools new smoke_table"
  "$bin" new smoke_table
  echo "CREATE TABLE smoke (id INTEGER PRIMARY KEY);" > migrations/*_smoke_table.up.sql

  echo "==> dbtools up"
  "$bin" up

  echo "==> dbtools status --json"
  "$bin" status --json

  echo "==> dbtools verify local --json"
  "$bin" verify local --json

  echo "==> dbtools lint"
  "$bin" lint

  echo "==> dbtools reset --yes"
  "$bin" reset --yes

  popd >/dev/null
  echo "==> smoke test OK"
}

cmd_all() {
  cmd_build
  cmd_lint
  cmd_test
  cmd_smoke
}

case "${1:-all}" in
  build) cmd_build ;;
  lint) cmd_lint ;;
  test) cmd_test ;;
  smoke) cmd_smoke ;;
  all) cmd_all ;;
  *)
    echo "unknown command: $1" >&2
    echo "usage: $0 {build|lint|test|smoke|all}" >&2
    exit 1
    ;;
esac
