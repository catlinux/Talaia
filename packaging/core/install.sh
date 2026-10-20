#!/usr/bin/env bash
#
# Sets up talaia-core with Docker Compose on this machine: writes .env with a
# random database password, builds and starts the stack, and creates the
# administrator. Run it from a clone of the repository; it is safe to run again.
#
# Usage:
#   packaging/core/install.sh [--hostname <ip-or-name>] [--port <port>] [--agent-port <port>] [--username <name>] [--no-user]
#
#   --hostname  address the panel is reached at (default: the first address of this machine)
#   --port      port of the panel published on this machine (default: 8443)
#   --agent-port port of the agents published on this machine (default: 8444)
#   --username  name of the administrator (default: admin)
#   --no-user   do not create the administrator
set -eu

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

hostname_arg=""
port="8443"
agent_port="8444"
username="admin"
create_user=1

while [ "$#" -gt 0 ]; do
    case "$1" in
        --hostname)
            [ "$#" -ge 2 ] || { echo "error: --hostname needs a value" >&2; exit 1; }
            hostname_arg="$2"
            shift 2
            ;;
        --port)
            [ "$#" -ge 2 ] || { echo "error: --port needs a value" >&2; exit 1; }
            port="$2"
            shift 2
            ;;
        --agent-port)
            [ "$#" -ge 2 ] || { echo "error: --agent-port needs a value" >&2; exit 1; }
            agent_port="$2"
            shift 2
            ;;
        --username)
            [ "$#" -ge 2 ] || { echo "error: --username needs a value" >&2; exit 1; }
            username="$2"
            shift 2
            ;;
        --no-user)
            create_user=0
            shift
            ;;
        -h | --help)
            sed -n '2,14p' "$SCRIPT_DIR/$(basename "${BASH_SOURCE[0]}")" | sed 's/^# \{0,1\}//'
            exit 0
            ;;
        *)
            echo "error: unknown option: $1" >&2
            exit 1
            ;;
    esac
done

for value in "$port" "$agent_port"; do
    case "$value" in
        '' | *[!0-9]*)
            echo "error: --port and --agent-port must be numbers" >&2
            exit 1
            ;;
    esac
done
if [ "$port" = "$agent_port" ]; then
    echo "error: --port and --agent-port must differ" >&2
    exit 1
fi

if ! command -v docker >/dev/null 2>&1; then
    echo "error: docker is required but was not found" >&2
    exit 1
fi
if ! docker compose version >/dev/null 2>&1 && ! sudo docker compose version >/dev/null 2>&1; then
    echo "error: the Docker Compose plugin is required" >&2
    exit 1
fi

# Use sudo only when the current user cannot talk to the Docker daemon.
if docker info >/dev/null 2>&1; then
    compose=(docker compose)
else
    compose=(sudo docker compose)
fi

# .env holds the database password: an existing one is never replaced, because
# the database volume was initialised with it.
if [ -f .env ]; then
    echo "keeping the existing .env"
else
    if [ -z "$hostname_arg" ]; then
        hostname_arg="$(hostname -I 2>/dev/null | cut -d' ' -f1 || true)"
    fi
    if [ -z "$hostname_arg" ]; then
        echo "error: could not find an address for this machine; pass --hostname" >&2
        exit 1
    fi

    password="$(od -An -tx1 -N32 /dev/urandom | tr -d ' \n')"
    (
        umask 077
        sed -e "s|^POSTGRES_PASSWORD=.*|POSTGRES_PASSWORD=${password}|" \
            -e "s|^TALAIA_PUBLIC_HOSTNAMES=.*|TALAIA_PUBLIC_HOSTNAMES=${hostname_arg}|" \
            -e "s|^TALAIA_PANEL_PORT=.*|TALAIA_PANEL_PORT=${port}|" \
            -e "s|^TALAIA_AGENT_PORT=.*|TALAIA_AGENT_PORT=${agent_port}|" \
            .env.example > .env
    )
    echo "wrote .env for $hostname_arg:$port"
fi

# shellcheck disable=SC1091
. ./.env
if [ -n "${TALAIA_PANEL_PORT:-}" ]; then
    port="$TALAIA_PANEL_PORT"
fi
if [ -n "${TALAIA_AGENT_PORT:-}" ]; then
    agent_port="$TALAIA_AGENT_PORT"
fi
if [ "$port" = "443" ]; then
    panel_url="https://${TALAIA_PUBLIC_HOSTNAMES%%,*}"
else
    panel_url="https://${TALAIA_PUBLIC_HOSTNAMES%%,*}:${port}"
fi

agent_url="https://${TALAIA_PUBLIC_HOSTNAMES%%,*}:${agent_port}"

echo "building and starting talaia-core (the first build takes a few minutes)"
"${compose[@]}" up -d --build

echo "waiting for the core to answer"
ready=0
for _ in $(seq 1 60); do
    if curl -fsk -o /dev/null "https://127.0.0.1:${port}/healthz" 2>/dev/null; then
        ready=1
        break
    fi
    sleep 2
done
if [ "$ready" -ne 1 ]; then
    echo "error: the core did not answer; see: ${compose[*]} logs core" >&2
    exit 1
fi

if [ "$create_user" -eq 1 ]; then
    users="$("${compose[@]}" exec -T db psql -U talaia -d talaia -tAc 'SELECT count(*) FROM app_user' 2>/dev/null | tr -d '[:space:]' || true)"
    if [ "$users" = "0" ]; then
        echo "creating the administrator \"$username\" (the password needs at least 12 characters)"
        "${compose[@]}" run --rm core user create --username "$username"
    else
        echo "an administrator already exists; not creating another"
    fi
fi

cat <<EOF

talaia-core is running.

  Panel: $panel_url
  Agents: $agent_url
  The browser warns about the certificate until you trust the Talaia CA:
  download ${agent_url}/ca.pem and install it as a trusted authority.

To add a server, create a token here and run the command it prints on that server:

  cd $SCRIPT_DIR && ${compose[*]} run --rm core token create --name <server-name>
EOF
