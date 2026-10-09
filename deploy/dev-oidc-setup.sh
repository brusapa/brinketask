#!/usr/bin/env bash
# Prepares the development Pocket ID (deploy/compose.dev.yaml) for logging
# in to brinketask, and prints a one-time login link:
#
#   - a user "dev" (dev@example.com);
#   - a confidential OIDC client "brinketask-dev" with PKCE and the
#     callbacks of the app in compose (port 8080) and of a server run on
#     the host (port 8081) and of the Vite dev server (port 5173; see
#     CLAUDE.md);
#   - deploy/dev.env with OIDC_CLIENT_ID and OIDC_CLIENT_SECRET for the app,
#     and a VAPID key pair for Web Push (D-68).
#
# Safe to run again: existing user, client and dev.env are kept; a new
# client secret is created only when dev.env has none or the client was
# recreated, and the VAPID keys only when dev.env has none (new keys would
# invalidate every browser subscription). Needs curl and Go. `make dev`
# runs it; deploy/e2e.sh runs it against its own Pocket ID with
# POCKET_ID_URL, OIDC_SETUP_ENV_FILE and OIDC_SETUP_CALLBACKS
# (space-separated callback URLs).
set -euo pipefail

pocket_id="${POCKET_ID_URL:-http://localhost:1411}"
# Fixed development value from deploy/compose.dev.yaml; not a secret.
api_key="brinketask-dev-static-api-key"
client_id="brinketask-dev"
# A fixed id keeps the script idempotent without searching.
user_id="00000000-0000-4000-8000-00000000d001"
env_file="${OIDC_SETUP_ENV_FILE:-$(dirname "$0")/dev.env}"
# The callbacks: the app in compose (8080), a server on the host (8081)
# and the Vite dev server in front of it (5173).
callbacks="${OIDC_SETUP_CALLBACKS:-http://localhost:8080/auth/callback http://localhost:8081/auth/callback http://localhost:5173/auth/callback}"

# api METHOD PATH [BODY] prints the response body and fails on HTTP errors
# other than 404, which callers test for with status.
api() {
  curl -sS --fail-with-body -X "$1" "$pocket_id/api$2" \
    -H "X-API-Key: $api_key" -H "Content-Type: application/json" \
    ${3:+--data "$3"}
}

status() {
  curl -s -o /dev/null -w '%{http_code}' "$pocket_id/api$1" -H "X-API-Key: $api_key"
}

# json_field NAME extracts a string field from the JSON on stdin. Enough
# for Pocket ID's flat responses; avoids depending on jq.
json_field() {
  sed -n "s/.*\"$1\":\"\([^\"]*\)\".*/\1/p"
}

# set_var NAME VALUE writes NAME=VALUE into dev.env, replacing an earlier
# value and keeping the other lines. umask keeps the file readable by its
# owner only.
set_var() {
  (umask 077 && touch "$env_file")
  local rest
  rest="$(grep -v "^$1=" "$env_file" || true)"
  (umask 077 && { [ -n "$rest" ] && printf '%s\n' "$rest"; printf '%s=%s\n' "$1" "$2"; } >"$env_file")
}

echo "waiting for Pocket ID at $pocket_id"
for _ in $(seq 60); do
  if curl -fs -o /dev/null "$pocket_id/healthz"; then
    break
  fi
  sleep 1
done

if [ "$(status "/users/$user_id")" = "404" ]; then
  echo "creating user dev"
  api POST /users "{\"id\":\"$user_id\",\"username\":\"dev\",\"email\":\"dev@example.com\",
    \"emailVerified\":true,\"firstName\":\"Dev\",\"lastName\":\"User\",\"displayName\":\"Dev User\",
    \"isAdmin\":false}" >/dev/null
fi

# As a JSON array: "a" "b" -> ["a","b"].
# $callbacks is unquoted on purpose: the shell splits it into one argument
# per URL.
callback_json="$(printf '"%s",' $callbacks)"
client_settings="\"name\":\"brinketask (dev)\",
  \"callbackURLs\":[${callback_json%,}],
  \"isPublic\":false,\"pkceEnabled\":true,\"skipConsent\":true"

new_client=false
if [ "$(status "/oidc/clients/$client_id")" = "404" ]; then
  echo "creating OIDC client $client_id"
  api POST /oidc/clients "{\"id\":\"$client_id\",$client_settings}" >/dev/null
  new_client=true
else
  # Keeps an existing client in step with the settings above.
  api PUT "/oidc/clients/$client_id" "{$client_settings}" >/dev/null
fi

if [ "$new_client" = true ] || ! grep -q "^OIDC_CLIENT_SECRET=" "$env_file" 2>/dev/null; then
  echo "creating a client secret in $env_file"
  secret="$(api POST "/oidc/clients/$client_id/secrets" '{}' | json_field secret)"
  if [ -z "$secret" ]; then
    echo "Pocket ID returned no client secret" >&2
    exit 1
  fi
  set_var OIDC_CLIENT_ID "$client_id"
  set_var OIDC_CLIENT_SECRET "$secret"
fi

if ! grep -q "^VAPID_PRIVATE_KEY=" "$env_file"; then
  echo "creating a VAPID key pair in $env_file"
  keys="$(cd "$(dirname "$0")/.." && go run ./cmd/brinketask vapid-keys)"
  set_var VAPID_PUBLIC_KEY "$(sed -n 's/^VAPID_PUBLIC_KEY=//p' <<<"$keys")"
  set_var VAPID_PRIVATE_KEY "$(sed -n 's/^VAPID_PRIVATE_KEY=//p' <<<"$keys")"
fi

token="$(api POST "/users/$user_id/one-time-access-token" '{}' | json_field token)"
echo
echo "Log in to Pocket ID as dev, no passkey needed (single use):"
echo "  $pocket_id/lc/$token"
echo "then open http://localhost:8080/auth/login"
