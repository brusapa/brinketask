#!/usr/bin/env bash
# Prepares the development Pocket ID (deploy/compose.dev.yaml) for logging
# in to brinketask, and prints a one-time login link:
#
#   - a user "dev" (dev@example.com);
#   - a confidential OIDC client "brinketask-dev" with PKCE and the
#     callbacks of the app in compose (port 8080) and of a server run on
#     the host (port 8081) and of the Vite dev server (port 5173; see
#     CLAUDE.md);
#   - deploy/dev.env with OIDC_CLIENT_ID and OIDC_CLIENT_SECRET for the app.
#
# Safe to run again: existing user, client and dev.env are kept; a new
# client secret is created only when dev.env is missing or the client was
# recreated. Needs curl. `make dev` runs it.
set -euo pipefail

pocket_id="${POCKET_ID_URL:-http://localhost:1411}"
# Fixed development value from deploy/compose.dev.yaml; not a secret.
api_key="brinketask-dev-static-api-key"
client_id="brinketask-dev"
# A fixed id keeps the script idempotent without searching.
user_id="00000000-0000-4000-8000-00000000d001"
env_file="$(dirname "$0")/dev.env"

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

# The callbacks: the app in compose (8080), a server on the host (8081)
# and the Vite dev server in front of it (5173).
client_settings="\"name\":\"brinketask (dev)\",
  \"callbackURLs\":[\"http://localhost:8080/auth/callback\",\"http://localhost:8081/auth/callback\",\"http://localhost:5173/auth/callback\"],
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

if [ "$new_client" = true ] || [ ! -s "$env_file" ]; then
  echo "creating a client secret in $env_file"
  secret="$(api POST "/oidc/clients/$client_id/secrets" '{}' | json_field secret)"
  if [ -z "$secret" ]; then
    echo "Pocket ID returned no client secret" >&2
    exit 1
  fi
  # umask keeps the file readable by its owner only.
  (umask 077 && printf 'OIDC_CLIENT_ID=%s\nOIDC_CLIENT_SECRET=%s\n' "$client_id" "$secret" >"$env_file")
fi

token="$(api POST "/users/$user_id/one-time-access-token" '{}' | json_field token)"
echo
echo "Log in to Pocket ID as dev, no passkey needed (single use):"
echo "  $pocket_id/lc/$token"
echo "then open http://localhost:8080/auth/login"
