#!/usr/bin/env bash
#
# Local development environment for Talaia.
#
# Starts PostgreSQL + TimescaleDB with Docker Compose and runs talaia-core, one
# talaia-agent and the web dev server (Vite) together. The output of each
# process is prefixed with [core], [agent] or [web].
#
# Usage:
#   scripts/dev.sh [up]        start the database and the services (default)
#   scripts/dev.sh down [-v]   stop the database; -v also removes its volume
#
# The database data lives in a named Docker volume and survives "down".
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

COMPOSE_FILE="$ROOT_DIR/packaging/dev/compose.yaml"

# Development values only, matching packaging/dev/compose.yaml.
DATABASE_URL="postgres://talaia:talaia-dev@127.0.0.1:5432/talaia?sslmode=disable"
CORE_URL="http://127.0.0.1:8080"
CORE_HEALTH_TIMEOUT=30

child_pids=()

usage() {
  echo "Usage: $(basename "$0") [up|down]" >&2
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "error: '$1' is required but was not found on PATH" >&2
    exit 1
  fi
}

# cleanup stops the services started by this script. The database is left
# running: use "dev.sh down" for that.
cleanup() {
  trap - INT TERM EXIT
  if [ "${#child_pids[@]}" -eq 0 ]; then
    return
  fi

  echo
  echo "stopping core, the agent and the web server"
  for pid in "${child_pids[@]}"; do
    kill "$pid" 2>/dev/null || true
  done
  wait 2>/dev/null || true
}

# start_child runs a command in the background and prefixes each output line
# with [name].
start_child() {
  local name="$1"
  shift
  "$@" > >(sed -u "s/^/[$name] /") 2>&1 &
  child_pids+=("$!")
}

# wait_for_health waits up to timeout seconds for the URL to return a 2xx.
wait_for_health() {
  local url="$1"
  local timeout="$2"
  local waited=0

  while [ "$waited" -lt "$timeout" ]; do
    if curl -fsS -o /dev/null "$url"; then
      return 0
    fi
    sleep 1
    waited=$((waited + 1))
  done

  return 1
}

run_up() {
  require_command docker
  require_command go
  require_command node
  require_command pnpm
  require_command curl

  echo "starting the database"
  docker compose -f "$COMPOSE_FILE" up -d --wait

  export TALAIA_DATABASE_URL="$DATABASE_URL"
  export TALAIA_CORE_URL="$CORE_URL"

  if [ ! -d "$ROOT_DIR/web/node_modules" ]; then
    echo "installing web dependencies"
    (cd "$ROOT_DIR/web" && pnpm install)
  fi

  trap cleanup INT TERM EXIT

  echo "starting core"
  start_child "core" bash -c 'cd "$1" && exec go run ./cmd/talaia-core' _ "$ROOT_DIR/core"

  if wait_for_health "$CORE_URL/healthz" "$CORE_HEALTH_TIMEOUT"; then
    echo "core is healthy"
  else
    echo "warning: core did not become healthy within ${CORE_HEALTH_TIMEOUT}s, starting the agent anyway" >&2
  fi

  echo "starting the agent"
  start_child "agent" bash -c 'cd "$1" && exec go run ./cmd/talaia-agent' _ "$ROOT_DIR/agent"

  echo "starting the web dev server"
  start_child "web" bash -c 'cd "$1" && exec pnpm dev' _ "$ROOT_DIR/web"

  echo "talaia is up, press Ctrl-C to stop"

  local status
  if wait -n; then
    status=0
  else
    status=$?
  fi

  if [ "$status" -eq 0 ]; then
    echo "a service stopped, shutting down" >&2
    exit 1
  fi
  echo "a service failed (status $status), shutting down" >&2
  exit "$status"
}

run_down() {
  require_command docker
  docker compose -f "$COMPOSE_FILE" down "$@"
}

command_name="${1:-up}"
shift || true

case "$command_name" in
  up)
    run_up "$@"
    ;;
  down)
    run_down "$@"
    ;;
  *)
    usage
    exit 2
    ;;
esac
