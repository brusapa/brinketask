#!/usr/bin/env bash
# Reproduction of finding L4 in REVIEW.md; it fails until the finding is
# fixed. Run it where with-test-postgres.sh runs (nix develop -c ...).
#
# A SIGTERM sent to the wrapper alone, as a process supervisor sends it,
# stops the command, the server and removes the directory promptly
# (D-75: "also on failure or Ctrl-C").
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

# The command records the server's directory and its own pid, then waits.
"$here/with-test-postgres.sh" bash -c \
  'echo "$TEST_DATABASE_URL" >"$0/url"; echo $$ >"$0/pid"; exec sleep 300' "$work" &
wrapper=$!

for _ in $(seq 100); do
  [ -s "$work/pid" ] && break
  sleep 0.1
done
[ -s "$work/pid" ] || { echo "FAIL: the command did not start"; exit 1; }
dir="$(sed -n 's/.*host=\([^&]*\).*/\1/p' "$work/url")"
child="$(cat "$work/pid")"

kill -TERM "$wrapper"
for _ in $(seq 50); do
  kill -0 "$wrapper" 2>/dev/null || break
  sleep 0.1
done

status=0
if kill -0 "$wrapper" 2>/dev/null; then
  echo "FAIL: the wrapper still runs 5 s after SIGTERM"
  status=1
fi
if kill -0 "$child" 2>/dev/null; then
  echo "FAIL: the command still runs"
  status=1
fi
if [ -d "$dir" ]; then
  echo "FAIL: $dir is still there"
  status=1
fi

# Leave nothing behind whatever happened.
kill "$child" 2>/dev/null || true
wait "$wrapper" 2>/dev/null || true
[ "$status" -eq 0 ] && echo "ok"
exit "$status"
