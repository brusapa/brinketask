#!/usr/bin/env bash
# End-to-end test (SPEC section 11): runs the built server binary with
# PostgreSQL, a Pocket ID and the fake push service (D-73) as local
# processes, no containers (D-75), then the Playwright tests in web/e2e
# against it.
#
# Everything listens on 127.0.0.1, so http://localhost:11411 is Pocket ID
# both for the browser and for the server: go-oidc requires the issuer to
# be the same URL for both. The ports differ from `make dev`'s, so both
# can run.
#
# PostgreSQL is the server of TEST_DATABASE_URL, in a database of its own;
# without the variable the script runs itself again under
# deploy/with-test-postgres.sh, which starts a throwaway one.
#
# Usage: deploy/e2e.sh [playwright arguments]; E2E_BINARY names the server
# binary (default bin/brinketask, which `make build` makes with the web
# client embedded). Needs Go, curl, psql, pocket-id (2.18.0, the version
# deploy/compose.dev.yaml runs) and the web dependencies (npm ci in web/):
# `nix develop` provides them all. The browser is Playwright's Chromium
# (npx playwright install chromium), or the one PLAYWRIGHT_CHROMIUM
# points to.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"

if [ -z "${TEST_DATABASE_URL:-}" ]; then
  exec "$root/deploy/with-test-postgres.sh" "$0" "$@"
fi

binary="${E2E_BINARY:-$root/bin/brinketask}"
app_url="http://localhost:18080"
pocket_id_url="http://localhost:11411"
fakepush_url="http://localhost:18091"
# Fixed value for a throwaway Pocket ID; the same as deploy/dev-oidc-setup.sh
# uses. Not a secret.
pocket_id_api_key="brinketask-dev-static-api-key"

if [ ! -x "$binary" ]; then
  echo "no server binary at $binary: run make build first" >&2
  exit 1
fi

work="$(mktemp -d)"
# The database of this run, on the server of TEST_DATABASE_URL.
database="e2e_$$"
# Process ids of the services this script starts, stopped by cleanup.
pids=()

cleanup() {
  status=$?
  for pid in "${pids[@]}"; do
    kill "$pid" 2>/dev/null || true
  done
  # wait collects the processes, so none outlives the script.
  for pid in "${pids[@]}"; do
    wait "$pid" 2>/dev/null || true
  done
  if [ "$status" -ne 0 ]; then
    for log in app fakepush pocket-id; do
      if [ -f "$work/$log.log" ]; then
        echo "--- $log log" >&2
        cat "$work/$log.log" >&2
      fi
    done
  fi
  psql "$TEST_DATABASE_URL" -qc "DROP DATABASE IF EXISTS $database WITH (FORCE)" >/dev/null 2>&1 || true
  rm -rf "$work"
  exit "$status"
}
trap cleanup EXIT

wait_for() { # wait_for URL WHAT
  for _ in $(seq 60); do
    if curl -fs -o /dev/null "$1"; then
      return 0
    fi
    sleep 1
  done
  echo "$2 did not come up at $1" >&2
  return 1
}

# with_database URL NAME prints URL pointing at the database NAME: the
# last path segment before the query is replaced. TEST_DATABASE_URL names a
# database, as with-test-postgres's does
# (postgresql:///postgres?host=/tmp/...).
with_database() {
  local base="${1%%\?*}"
  local query="${1#"$base"}"
  printf '%s/%s%s' "${base%/*}" "$2" "$query"
}

# A service left over from another run would answer in place of the ones
# this script starts, and wait_for could not tell.
for url in "$app_url" "$pocket_id_url" "$fakepush_url"; do
  if curl -s -o /dev/null "$url"; then
    echo "something already listens at $url; stop it first" >&2
    exit 1
  fi
done

echo "building the fake push service"
(cd "$root" && CGO_ENABLED=0 go build -trimpath -o "$work/fakepush" ./cmd/fakepush)

psql "$TEST_DATABASE_URL" -qc "CREATE DATABASE $database" >/dev/null

# Pocket ID keeps its SQLite database and uploads under data/ in its
# working directory.
mkdir "$work/pocket-id"
(
  cd "$work/pocket-id"
  exec env APP_URL="$pocket_id_url" HOST=127.0.0.1 PORT=11411 \
    ENCRYPTION_KEY=brinketask-e2e-encryption-key \
    STATIC_API_KEY="$pocket_id_api_key" \
    TRUST_PROXY=false ANALYTICS_DISABLED=true \
    pocket-id
) >"$work/pocket-id.log" 2>&1 &
pids+=("$!")
wait_for "$pocket_id_url/healthz" "Pocket ID"

# The user, the OIDC client and the VAPID keys, written to $work/e2e.env.
POCKET_ID_URL="$pocket_id_url" OIDC_SETUP_ENV_FILE="$work/e2e.env" \
  OIDC_SETUP_CALLBACKS="$app_url/auth/callback" \
  "$root/deploy/dev-oidc-setup.sh" >/dev/null
# set -a exports every variable the file assigns.
set -a
. "$work/e2e.env"
set +a

# A one-second scheduler so the reminder arrives quickly, and the fake push
# service's prefix (D-73). The environment is passed explicitly with env -i
# so nothing else from this shell reaches the server.
env -i PATH="$PATH" \
  DATABASE_URL="$(with_database "$TEST_DATABASE_URL" "$database")" \
  LISTEN_ADDR=127.0.0.1:18080 METRICS_LISTEN_ADDR= \
  PUBLIC_URL="$app_url" OIDC_ISSUER="$pocket_id_url" \
  OIDC_CLIENT_ID="$OIDC_CLIENT_ID" OIDC_CLIENT_SECRET="$OIDC_CLIENT_SECRET" \
  VAPID_PUBLIC_KEY="$VAPID_PUBLIC_KEY" VAPID_PRIVATE_KEY="$VAPID_PRIVATE_KEY" \
  VAPID_SUBJECT=mailto:e2e@example.com \
  SCHEDULER_INTERVAL=1s PUSH_TEST_ENDPOINT_PREFIX="$fakepush_url/" \
  "$binary" >"$work/app.log" 2>&1 &
pids+=("$!")

env -i VAPID_PUBLIC_KEY="$VAPID_PUBLIC_KEY" \
  FAKEPUSH_LISTEN_ADDR=127.0.0.1:18091 FAKEPUSH_URL="$fakepush_url" \
  "$work/fakepush" >"$work/fakepush.log" 2>&1 &
pids+=("$!")

wait_for "$app_url/healthz" "the application"
wait_for "$fakepush_url/messages" "the fake push service"

echo "running Playwright"
cd "$root/web"
E2E_BASE_URL="$app_url" E2E_POCKET_ID_URL="$pocket_id_url" \
  E2E_POCKET_ID_API_KEY="$pocket_id_api_key" E2E_FAKEPUSH_URL="$fakepush_url" \
  npx playwright test "$@"
