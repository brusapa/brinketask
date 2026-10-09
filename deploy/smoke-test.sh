#!/usr/bin/env bash
# Smoke test of a built application image: starts PostgreSQL and the image
# with the production restrictions (read-only root, no capabilities) and
# checks that the server migrates, answers GET /healthz with 200, refuses
# anonymous API calls, serves the login route and serves the web client.
# The OIDC provider is a dummy: discovery runs on the first login, not at
# startup, and the health check does not depend on it.
#
# Usage: deploy/smoke-test.sh [image]   (default localhost/brinketask:dev)
# Uses Podman unless CONTAINER_ENGINE says otherwise.
set -euo pipefail

image="${1:-localhost/brinketask:dev}"
engine="${CONTAINER_ENGINE:-podman}"
postgres_image="docker.io/library/postgres:18.6-alpine3.24"
name="brinketask-smoke-$$"
port=18080

cleanup() {
  "$engine" rm -f "$name-app" "$name-db" >/dev/null 2>&1 || true
  "$engine" network rm "$name" >/dev/null 2>&1 || true
}
trap cleanup EXIT

"$engine" network create "$name" >/dev/null
"$engine" run -d --name "$name-db" --network "$name" \
  -e POSTGRES_USER=smoke -e POSTGRES_PASSWORD=smoke -e POSTGRES_DB=smoke \
  "$postgres_image" >/dev/null

# -h 127.0.0.1 checks TCP: the image first runs a temporary socket-only
# server for initialisation, which must not count as ready.
echo "waiting for PostgreSQL"
for _ in $(seq 60); do
  if "$engine" exec "$name-db" pg_isready -h 127.0.0.1 -U smoke -d smoke >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

# A throwaway VAPID key pair, made by the image itself (D-68).
keys="$("$engine" run --rm "$image" vapid-keys)"
vapid_public="$(sed -n 's/^VAPID_PUBLIC_KEY=//p' <<<"$keys")"
vapid_private="$(sed -n 's/^VAPID_PRIVATE_KEY=//p' <<<"$keys")"

"$engine" run -d --name "$name-app" --network "$name" \
  --read-only --cap-drop=ALL -p "127.0.0.1:$port:8080" \
  -e DATABASE_URL="postgres://smoke:smoke@$name-db:5432/smoke?sslmode=disable" \
  -e PUBLIC_URL="http://localhost:$port" \
  -e OIDC_ISSUER="http://127.0.0.1:1" \
  -e OIDC_CLIENT_ID=smoke -e OIDC_CLIENT_SECRET=smoke \
  -e VAPID_PUBLIC_KEY="$vapid_public" -e VAPID_PRIVATE_KEY="$vapid_private" \
  -e VAPID_SUBJECT=mailto:smoke@example.com \
  "$image" >/dev/null

fail() {
  echo "smoke test failed: $1" >&2
  "$engine" logs "$name-app" >&2 || true
  exit 1
}

echo "waiting for GET /healthz"
status=""
for _ in $(seq 30); do
  status="$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$port/healthz" || true)"
  [ "$status" = "200" ] && break
  sleep 1
done
[ "$status" = "200" ] || fail "last /healthz status ${status:-none}"

# The API is mounted and refuses anonymous callers.
status="$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$port/api/v1/me")"
[ "$status" = "401" ] || fail "GET /api/v1/me without a session answered $status, want 401"

# The login route is mounted; with the dummy provider it reports it
# unavailable instead of failing.
location="$(curl -s -o /dev/null -w '%{redirect_url}' "http://127.0.0.1:$port/auth/login")"
case "$location" in
  */?auth_error=provider_unavailable) ;;
  *) fail "GET /auth/login redirected to '$location', want /?auth_error=provider_unavailable" ;;
esac

# The web client is embedded and served with its security headers.
headers="$(curl -s -D - -o /dev/null "http://127.0.0.1:$port/today")"
grep -qi "^content-type: text/html" <<<"$headers" || fail "GET /today is not HTML"
grep -qi "^content-security-policy: default-src 'self'" <<<"$headers" || fail "GET /today has no CSP"

echo "smoke test passed"
