#!/usr/bin/env bash
set -euo pipefail

binary="${1:-dist/latasya-erp}"
expected_version="${2:-}"
smoke_port="${SMOKE_PORT:-18081}"
smoke_directory=$(mktemp -d "${TMPDIR:-/tmp}/latasya-smoke.XXXXXX")
server_pid=""

cleanup() {
  if [ -n "$server_pid" ] && kill -0 "$server_pid" 2>/dev/null; then
    kill -TERM "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  rm -rf -- "$smoke_directory"
}
trap cleanup EXIT INT TERM

DB_PATH="$smoke_directory/latasya.db" \
PORT="$smoke_port" \
DEV_MODE=true \
"$binary" >"$smoke_directory/server.log" 2>&1 &
server_pid=$!

body=""
for _ in $(seq 1 30); do
  if ! kill -0 "$server_pid" 2>/dev/null; then
    cat "$smoke_directory/server.log"
    exit 1
  fi
  if body=$(curl -fsS "http://127.0.0.1:$smoke_port/healthz" 2>/dev/null); then
    break
  fi
  sleep 0.2
done

if [ -z "$body" ]; then
  cat "$smoke_directory/server.log"
  exit 1
fi
if [ -n "$expected_version" ] &&
  [[ "$body" != *"version=$expected_version"* ]]; then
  printf 'unexpected health response: %s\n' "$body"
  exit 1
fi

printf '%s\n' "$body"
kill -TERM "$server_pid"
wait "$server_pid"
server_pid=""
