#!/usr/bin/env bash
# End-to-end suite, run from any directory:
#
#   1. starts a throwaway PostgreSQL container (Docker required);
#   2. runs every test, including the integration-tagged ones, against it;
#   3. squashes each fixture at every safety level that needs no production
#      database and proves, with `validate-external` (the contract the CapyDB
#      CLI uses), that the squashed baseline builds the same catalog as the
#      original history.
#
# CAPYSQUASH_E2E_POSTGRES_IMAGE picks the server (default postgres:18).
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_root"

# The module is self-contained; do not resolve through a surrounding go.work.
export GOWORK=off

image="${CAPYSQUASH_E2E_POSTGRES_IMAGE:-postgres:18}"
container="capysquash-e2e-$$"
work="$(mktemp -d)"

cleanup() {
  docker rm -f "$container" >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT

echo "Starting $image..."
docker run -d --rm --name "$container" -e POSTGRES_PASSWORD=postgres \
  -p 127.0.0.1::5432 "$image" >/dev/null
port="$(docker port "$container" 5432/tcp | head -n 1 | sed 's/.*://')"

for _ in $(seq 1 60); do
  # pg_isready over TCP: the entrypoint's temporary server only listens on
  # the socket, so this waits for the real one.
  if docker exec "$container" pg_isready -h 127.0.0.1 -U postgres >/dev/null 2>&1; then
    break
  fi
  sleep 1
done
docker exec "$container" pg_isready -h 127.0.0.1 -U postgres >/dev/null

export DATABASE_URL="postgres://postgres:postgres@127.0.0.1:${port}/postgres?sslmode=disable"

echo "Running the test suite with the integration tests..."
go test -race -timeout 30m -tags=integration ./...

echo "Building capysquash..."
go build -o "$work/capysquash" ./cmd/capysquash

# Fixtures whose original history PostgreSQL itself rejects cannot be
# compared; each is listed with the statement that fails. (A function, not an
# associative array: macOS still ships bash 3.2.)
excluded_reason() {
  case "$1" in
    enums_append_reorder) echo '001_create.sql uses type "user_status" before creating it' ;;
    generated_columns_identity) echo '002_generated.sql uses a generated column in another generation expression' ;;
    matviews) echo '004_manual_refresh.sql refreshes CONCURRENTLY a materialized view created WITH NO DATA' ;;
    rls_policies) echo '003_setup_rls.sql calls current_user_id(), which no migration creates' ;;
  esac
}

export CAPYSQUASH_E2E_DSN="postgres://postgres:postgres@127.0.0.1:${port}/capysquash_e2e?sslmode=disable"

fresh_database() {
  docker exec "$container" dropdb -U postgres --if-exists capysquash_e2e
  docker exec "$container" createdb -U postgres capysquash_e2e
}

validate() {
  "$work/capysquash" validate-external "$1" --dsn-env CAPYSQUASH_E2E_DSN \
    "$2" "$3" --json --quiet --no-emoji 2>"$work/validate.log"
}

failures=0
for dir in test-fixtures/*/original; do
  fixture="$(basename "$(dirname "$dir")")"
  reason="$(excluded_reason "$fixture")"
  if [[ -n "$reason" ]]; then
    echo "SKIP $fixture: $reason"
    continue
  fi

  fresh_database
  if ! validate "$dir" --snapshot-output "$work/original.json" | grep -q '"success":true'; then
    echo "FAIL $fixture: the original history does not apply (see validate-external output)"
    failures=$((failures + 1))
    continue
  fi

  for level in conservative standard aggressive; do
    out="$work/$fixture-$level"
    if ! "$work/capysquash" squash "$dir"/*.sql --output "$out" --safety "$level" \
      --no-validate --i-know-what-im-doing --quiet --no-emoji >/dev/null 2>"$work/squash.log"; then
      echo "FAIL $fixture ($level): squash failed"
      tail -n 20 "$work/squash.log"
      failures=$((failures + 1))
      continue
    fi

    fresh_database
    result="$(validate "$out" --against-snapshot "$work/original.json" || true)"
    if grep -q '"success":true' <<<"$result" && grep -q '"has_differences":false' <<<"$result"; then
      echo "PASS $fixture ($level)"
    else
      echo "FAIL $fixture ($level): the squashed baseline differs from the original"
      echo "$result"
      failures=$((failures + 1))
    fi
  done
done

if ((failures > 0)); then
  echo "$failures end-to-end check(s) failed"
  exit 1
fi
echo "End-to-end suite passed"
