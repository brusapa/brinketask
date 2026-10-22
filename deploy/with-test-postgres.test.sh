#!/usr/bin/env bash
# Tests that deploy/with-test-postgres.sh cleans up when interrupted
# (D-75): the command and PostgreSQL stop and the directory goes, within
# seconds, both when only the wrapper is signalled (a process supervisor,
# a CI runner) and on Ctrl-C (SIGINT to the whole process group). Run it
# where the wrapper runs: nix develop -c deploy/with-test-postgres.test.sh
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
failed=0

# check SIGNAL TARGET: TARGET is "wrapper" (its pid) or "group" (its
# process group, as a terminal's Ctrl-C does).
check() {
  local signal="$1" target="$2" work wrapper dir child
  work="$(mktemp -d)"
  # Job control (set -m) starts the wrapper as a terminal would: in a
  # process group of its own, to signal, and with SIGINT not ignored (a
  # script's background jobs ignore it otherwise, and bash cannot trap a
  # signal ignored on entry). The command records the server's directory
  # and its pid, then waits.
  set -m
  "$here/with-test-postgres.sh" bash -c \
    'echo "$TEST_DATABASE_URL" >"$0/url"; echo $$ >"$0/pid"; exec sleep 300' "$work" &
  wrapper=$!
  set +m
  for _ in $(seq 100); do
    [ -s "$work/pid" ] && break
    sleep 0.1
  done
  if [ ! -s "$work/pid" ]; then
    echo "FAIL ($signal to $target): the command did not start"
    failed=1
    return
  fi
  dir="$(sed -n 's/.*host=\([^&]*\).*/\1/p' "$work/url")"
  child="$(cat "$work/pid")"

  if [ "$target" = group ]; then
    kill -"$signal" -- "-$wrapper"
  else
    kill -"$signal" "$wrapper"
  fi
  for _ in $(seq 50); do
    kill -0 "$wrapper" 2>/dev/null || break
    sleep 0.1
  done

  local ok=1
  if kill -0 "$wrapper" 2>/dev/null; then
    echo "FAIL ($signal to $target): the wrapper still runs after 5 s"
    ok=0
  fi
  if kill -0 "$child" 2>/dev/null; then
    echo "FAIL ($signal to $target): the command still runs"
    ok=0
  fi
  if [ -d "$dir" ]; then
    echo "FAIL ($signal to $target): $dir is still there"
    ok=0
  fi
  [ "$ok" -eq 1 ] && echo "ok: $signal to $target"
  [ "$ok" -eq 1 ] || failed=1

  # Leave nothing behind whatever happened.
  kill "$child" 2>/dev/null || true
  wait "$wrapper" 2>/dev/null || true
  rm -rf "$work"
}

check TERM wrapper
check INT wrapper
check INT group
exit "$failed"
