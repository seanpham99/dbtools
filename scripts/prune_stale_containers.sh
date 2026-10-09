#!/usr/bin/env bash
# Reaps dbtools-owned database containers two ways:
#
#   1. idle-but-live: running postgres containers older than MIN_AGE_HOURS
#      with zero active connections (stopgap for containers whose project
#      still exists but nothing needs right now — `dbtools start` recreates
#      them cheaply).
#   2. dead-owner: `dbtools prune --volumes` removes any labeled container
#      and data volume whose dbtools.toml no longer exists — the git-
#      worktree-deleted leak that started this.
#
# The dbtools binary is resolved as ../dbtools relative to this script (the
# repo's gitignored build output), NOT via PATH — ~/.local/bin/dbtools is an
# older npm-installed release that lacks `prune`.
#
# Install: crontab -e
#   17 */6 * * * <repo>/scripts/prune_stale_containers.sh >>/tmp/dbtools-prune.log 2>&1
set -euo pipefail

DBTOOLS="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/dbtools"
MIN_AGE_HOURS="${MIN_AGE_HOURS:-2}"
NOW=$(date +%s)

for name in $(docker ps --format '{{.Names}}' | grep -E '^dbtools-postgres-[0-9a-f]{8}$' || true); do
    started=$(docker inspect "$name" --format '{{.State.StartedAt}}' | cut -d. -f1)
    age_h=$(( (NOW - $(date -d "$started" +%s)) / 3600 ))
    if [ "$age_h" -lt "$MIN_AGE_HOURS" ]; then
        continue
    fi
    conns=$(docker exec "$name" psql -U postgres -tAc \
        "select count(*) from pg_stat_activity where state='active' and pid<>pg_backend_pid()" \
        2>/dev/null || echo -1)
    # a failed check (-1) means "don't touch" — never reap blind.
    if [ "$conns" = "0" ]; then
        docker rm -f "$name" >/dev/null
        echo "$(date -Is) reaped $name (age=${age_h}h, idle)"
    else
        echo "$(date -Is) kept   $name (age=${age_h}h, conns=$conns)"
    fi
done

if [ -x "$DBTOOLS" ]; then
    "$DBTOOLS" prune --volumes
else
    echo "$(date -Is) WARN: $DBTOOLS missing — run 'go build -o dbtools .' in the repo; dead-owner prune skipped"
fi
