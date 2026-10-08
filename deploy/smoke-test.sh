#!/usr/bin/env bash
# Smoke test of a built application image: starts PostgreSQL and the image
# with the production restrictions (read-only root, no capabilities) and
# checks that the server migrates and answers GET /healthz with 200.
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

"$engine" run -d --name "$name-app" --network "$name" \
  --read-only --cap-drop=ALL -p "127.0.0.1:$port:8080" \
  -e DATABASE_URL="postgres://smoke:smoke@$name-db:5432/smoke?sslmode=disable" \
  -e PUBLIC_URL="http://localhost:$port" \
  -e OIDC_ISSUER="http://127.0.0.1:1" \
  -e OIDC_CLIENT_ID=smoke -e OIDC_CLIENT_SECRET=smoke \
  "$image" >/dev/null

echo "waiting for GET /healthz"
for _ in $(seq 30); do
  status="$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:$port/healthz" || true)"
  if [ "$status" = "200" ]; then
    echo "smoke test passed"
    exit 0
  fi
  sleep 1
done

echo "smoke test failed: last /healthz status ${status:-none}" >&2
"$engine" logs "$name-app" >&2 || true
exit 1
