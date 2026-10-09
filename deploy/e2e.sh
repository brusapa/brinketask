#!/usr/bin/env bash
# End-to-end test (SPEC section 11): runs the built application image with
# PostgreSQL, a Pocket ID and the fake push service (D-73) in one Podman
# pod, then the Playwright tests in web/e2e against it.
#
# In a pod the containers share one network namespace, so
# http://localhost:11411 is Pocket ID both for the browser on the host and
# for the server in the pod: go-oidc requires the issuer to be the same
# URL for both. The ports differ from `make dev`'s, so both can run.
#
# Usage: deploy/e2e.sh [playwright arguments]; E2E_IMAGE names the image
# (default localhost/brinketask:dev).
# Needs Podman, Go, curl and the web dependencies (npm ci in web/). The
# browser is Playwright's Chromium (npx playwright install chromium), or
# the one PLAYWRIGHT_CHROMIUM points to.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
image="${E2E_IMAGE:-localhost/brinketask:dev}"
postgres_image="docker.io/library/postgres:18.6-alpine3.24"
# Keep in step with deploy/compose.dev.yaml.
pocket_id_image="ghcr.io/pocket-id/pocket-id:v2.18.0@sha256:323e7ef5bfacf7cf32f22f3d10ccdf48e0c97c94d67e7e40b0b996830deb5165"
pod="brinketask-e2e-$$"
app_url="http://localhost:18080"
pocket_id_url="http://localhost:11411"
fakepush_url="http://localhost:18091"
# Fixed value for a throwaway Pocket ID; the same as deploy/dev-oidc-setup.sh
# uses. Not a secret.
pocket_id_api_key="brinketask-dev-static-api-key"

work="$(mktemp -d)"
cleanup() {
  status=$?
  if [ "$status" -ne 0 ]; then
    echo "--- application log" >&2
    podman logs "$pod-app" >&2 2>&1 || true
    echo "--- fake push service log" >&2
    podman logs "$pod-push" >&2 2>&1 || true
  fi
  podman pod rm -f "$pod" >/dev/null 2>&1 || true
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

echo "building the fake push service"
(cd "$root" && CGO_ENABLED=0 go build -trimpath -o "$work/fakepush" ./cmd/fakepush)

podman pod create --name "$pod" \
  -p 127.0.0.1:18080:18080 -p 127.0.0.1:11411:11411 -p 127.0.0.1:18091:18091 >/dev/null

podman run -d --pod "$pod" --name "$pod-db" \
  -e POSTGRES_USER=e2e -e POSTGRES_PASSWORD=e2e -e POSTGRES_DB=e2e \
  "$postgres_image" >/dev/null

podman run -d --pod "$pod" --name "$pod-idp" \
  -e APP_URL="$pocket_id_url" -e PORT=11411 \
  -e ENCRYPTION_KEY=brinketask-e2e-encryption-key \
  -e STATIC_API_KEY="$pocket_id_api_key" \
  -e TRUST_PROXY=false -e ANALYTICS_DISABLED=true \
  "$pocket_id_image" >/dev/null
wait_for "$pocket_id_url/healthz" "Pocket ID"

# The user, the OIDC client and the VAPID keys, written to $work/e2e.env.
POCKET_ID_URL="$pocket_id_url" OIDC_SETUP_ENV_FILE="$work/e2e.env" \
  OIDC_SETUP_CALLBACKS="$app_url/auth/callback" \
  "$root/deploy/dev-oidc-setup.sh" >/dev/null
# set -a exports every variable the file assigns.
set -a
. "$work/e2e.env"
set +a

echo "waiting for PostgreSQL"
for _ in $(seq 60); do
  if podman exec "$pod-db" pg_isready -h 127.0.0.1 -U e2e -d e2e >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

# The production restrictions, a one-second scheduler so the reminder
# arrives quickly, and the fake push service's prefix (D-73).
podman run -d --pod "$pod" --name "$pod-app" --read-only --cap-drop=ALL \
  -e DATABASE_URL="postgres://e2e:e2e@localhost:5432/e2e?sslmode=disable" \
  -e LISTEN_ADDR=:18080 -e METRICS_LISTEN_ADDR= \
  -e PUBLIC_URL="$app_url" -e OIDC_ISSUER="$pocket_id_url" \
  -e OIDC_CLIENT_ID -e OIDC_CLIENT_SECRET \
  -e VAPID_PUBLIC_KEY -e VAPID_PRIVATE_KEY -e VAPID_SUBJECT=mailto:e2e@example.com \
  -e SCHEDULER_INTERVAL=1s -e PUSH_TEST_ENDPOINT_PREFIX="$fakepush_url/" \
  "$image" >/dev/null

# The fake push service runs in the application image, which has no shell
# or libraries a static binary needs.
podman run -d --pod "$pod" --name "$pod-push" --read-only --cap-drop=ALL \
  -v "$work/fakepush:/fakepush:ro,Z" --entrypoint /fakepush \
  -e VAPID_PUBLIC_KEY -e FAKEPUSH_LISTEN_ADDR=:18091 -e FAKEPUSH_URL="$fakepush_url" \
  "$image" >/dev/null

wait_for "$app_url/healthz" "the application"
wait_for "$fakepush_url/messages" "the fake push service"

echo "running Playwright"
cd "$root/web"
E2E_BASE_URL="$app_url" E2E_POCKET_ID_URL="$pocket_id_url" \
  E2E_POCKET_ID_API_KEY="$pocket_id_api_key" E2E_FAKEPUSH_URL="$fakepush_url" \
  npx playwright test "$@"
