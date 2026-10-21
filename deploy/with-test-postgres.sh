#!/usr/bin/env bash
# Runs a command next to a throwaway PostgreSQL (D-75):
#
#   deploy/with-test-postgres.sh make go-test
#
# The server lives in a new temporary directory, listens on a Unix socket
# in that directory only (no TCP port) and trusts every local connection,
# which only the owner of the directory can open. The command gets the
# superuser's connection URL in TEST_DATABASE_URL. When the command ends,
# or the script is interrupted, the server is stopped and the directory
# removed; the script exits with the command's status.
#
# Needs initdb, pg_ctl and postgres 18 or later on the PATH. The flake's
# development shell provides them and this script as `with-test-postgres`
# (nix develop -c with-test-postgres make go-test).
set -euo pipefail

if [ "$#" -eq 0 ]; then
  echo "usage: with-test-postgres COMMAND [ARGUMENT...]" >&2
  exit 2
fi

# "pg_ctl (PostgreSQL) 18.6" -> 18. The schema uses uuidv7(), new in 18.
major="$(pg_ctl --version | sed -n 's/^pg_ctl (PostgreSQL) \([0-9]*\).*/\1/p')"
if [ -z "$major" ] || [ "$major" -lt 18 ]; then
  echo "with-test-postgres: PostgreSQL 18 or later required, found: $(pg_ctl --version)" >&2
  exit 1
fi

# Always under /tmp, never $TMPDIR: the socket path, <dir>/.s.PGSQL.5432,
# must stay within the 107 bytes a Unix socket address can hold.
dir="$(mktemp -d /tmp/brinketask-pg.XXXXXX)"
started=0
cleanup() {
  status=$?
  if [ "$started" -eq 1 ]; then
    # immediate: nothing to save, so no checkpoint at shutdown.
    pg_ctl stop -D "$dir/data" -m immediate >/dev/null 2>&1 || true
  fi
  rm -rf "$dir"
  exit "$status"
}
trap cleanup EXIT
# A signal ends the script through the EXIT trap above, with the usual
# 128 + signal status.
trap 'exit 130' INT
trap 'exit 143' TERM

initdb -D "$dir/data" -U postgres --auth=trust --encoding=UTF8 --no-locale >"$dir/initdb.log" 2>&1 || {
  cat "$dir/initdb.log" >&2
  exit 1
}

# fsync and the like are off: the data is thrown away, so durability only
# costs time.
if ! pg_ctl start -D "$dir/data" -w -l "$dir/postgres.log" \
  -o "-c listen_addresses='' -c unix_socket_directories='$dir' -c fsync=off -c synchronous_commit=off -c full_page_writes=off -c max_connections=200" >/dev/null; then
  cat "$dir/postgres.log" >&2
  exit 1
fi
started=1

export TEST_DATABASE_URL="postgresql:///postgres?host=$dir&user=postgres"
# Not exec: the trap above must run after the command.
"$@"
