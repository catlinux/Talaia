#!/usr/bin/env bash
#
# Sets up talaia-core with Docker Compose on this machine: writes .env with a
# random database password, builds and starts the stack, and creates the
# administrator. Run it from a clone of the repository; it is safe to run again.
#
# Usage:
#   packaging/core/install.sh [--hostname <ip-or-name>] [--port <port>] [--agent-port <port>]
#                             [--acme <provider>] [--acme-email <address>] [--username <name>] [--no-user]
#
#   --hostname    name or address the core is reached at (asked for in a terminal; default: the first address of this machine)
#   --port        port of the panel (default: the first free one from 8443; 80 and 443 are never chosen on their own)
#   --agent-port  port of the agents (default: the first free one from 8444)
#   --acme        trusted certificate for the panel by ACME DNS-01 with this DNS provider (ovh); the
#                 credentials come from the environment (OVH_*) or are asked for, and go to .env.acme
#   --acme-email  contact address for the certificate authority
#   --username    name of the administrator (default: admin)
#   --no-user     do not create the administrator
#   --no-hostd    do not install talaia-hostd, the root helper that applies updates requested from the panel
set -eu

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

hostname_arg=""
port=""
agent_port=""
acme_provider=""
acme_email=""
username="admin"
create_user=1
install_helper=1

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
        --acme)
            [ "$#" -ge 2 ] || { echo "error: --acme needs a value" >&2; exit 1; }
            acme_provider="$2"
            shift 2
            ;;
        --acme-email)
            [ "$#" -ge 2 ] || { echo "error: --acme-email needs a value" >&2; exit 1; }
            acme_email="$2"
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
        --no-hostd)
            install_helper=0
            shift
            ;;
        -h | --help)
            sed -n '2,19p' "$SCRIPT_DIR/$(basename "${BASH_SOURCE[0]}")" | sed 's/^# \{0,1\}//'
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
        *[!0-9]*)
            echo "error: --port and --agent-port must be numbers" >&2
            exit 1
            ;;
    esac
done
if [ -n "$port" ] && [ "$port" = "$agent_port" ]; then
    echo "error: --port and --agent-port must differ" >&2
    exit 1
fi
case "$acme_provider" in
    '' | ovh) ;;
    *)
        echo "error: unsupported ACME provider $acme_provider (supported: ovh)" >&2
        exit 1
        ;;
esac

if ! command -v docker >/dev/null 2>&1; then
    echo "error: docker is required but was not found" >&2
    exit 1
fi
if ! docker compose version >/dev/null 2>&1 && ! sudo docker compose version >/dev/null 2>&1; then
    echo "error: the Docker Compose plugin is required" >&2
    exit 1
fi

# The commit is stamped into the core so the panel can tell which updates are new.
git_commit="$(git -C "$SCRIPT_DIR" rev-parse HEAD 2>/dev/null || true)"

# Use sudo only when the current user cannot talk to the Docker daemon.
if docker info >/dev/null 2>&1; then
    compose=(env "TALAIA_GIT_COMMIT=${git_commit}" docker compose)
else
    compose=(sudo env "TALAIA_GIT_COMMIT=${git_commit}" docker compose)
fi

# port_in_use reports whether something already listens on a TCP port.
port_in_use() {
    if command -v ss >/dev/null 2>&1; then
        [ -n "$(ss -ltnH "sport = :$1" 2>/dev/null)" ]
    else
        (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null
    fi
}

# pick_port prints the first free port from $1 up, skipping 80, 443 and $2.
pick_port() {
    local candidate="$1" last=$(($1 + 200))
    while [ "$candidate" -le "$last" ]; do
        if [ "$candidate" != 80 ] && [ "$candidate" != 443 ] && [ "$candidate" != "${2:-}" ] && ! port_in_use "$candidate"; then
            echo "$candidate"
            return 0
        fi
        candidate=$((candidate + 1))
    done
    echo "error: no free port found from $1; pass --port and --agent-port" >&2
    return 1
}

# ask prints the answer to a question, or the default when it is empty.
ask() {
    local answer
    read -r -p "$1 [$2]: " answer
    echo "${answer:-$2}"
}

# write_acme_credentials stores the DNS provider credentials in .env.acme (mode
# 0600), which compose hands to the core. Values come from the environment or,
# in a terminal, from a prompt that does not echo secrets.
write_acme_credentials() {
    if [ -f .env.acme ]; then
        echo "keeping the existing .env.acme"
        return 0
    fi
    local endpoint="${OVH_ENDPOINT:-}" key="${OVH_APPLICATION_KEY:-}" secret="${OVH_APPLICATION_SECRET:-}" consumer="${OVH_CONSUMER_KEY:-}"
    if [ -t 0 ]; then
        [ -n "$endpoint" ] || endpoint="$(ask "OVH endpoint" "ovh-eu")"
        [ -n "$key" ] || read -r -p "OVH application key: " key
        if [ -z "$secret" ]; then
            read -r -s -p "OVH application secret: " secret
            echo
        fi
        if [ -z "$consumer" ]; then
            read -r -s -p "OVH consumer key: " consumer
            echo
        fi
    fi
    : "${endpoint:=ovh-eu}"
    if [ -z "$key" ] || [ -z "$secret" ] || [ -z "$consumer" ]; then
        echo "error: set OVH_APPLICATION_KEY, OVH_APPLICATION_SECRET and OVH_CONSUMER_KEY (or run in a terminal)" >&2
        exit 1
    fi
    (
        umask 077
        printf 'OVH_ENDPOINT=%s\nOVH_APPLICATION_KEY=%s\nOVH_APPLICATION_SECRET=%s\nOVH_CONSUMER_KEY=%s\n' \
            "$endpoint" "$key" "$secret" "$consumer" > .env.acme
    )
    echo "wrote .env.acme"
}

# install_hostd sets up talaia-hostd, the root helper that applies the updates
# requested from the panel: the core only gets a directory to drop requests in,
# never the Docker socket. It runs before the stack starts, so Docker finds the
# shared directory with the right owners instead of creating it.
install_hostd() {
    local repo_dir repo_user as_root=()
    repo_dir="$(cd "$SCRIPT_DIR/../.." && pwd)"
    if ! command -v systemctl >/dev/null 2>&1 || [ ! -d /run/systemd/system ]; then
        echo "warning: no systemd here; updates from the panel stay off" >&2
        return 0
    fi
    if [ ! -d "$repo_dir/.git" ]; then
        echo "warning: $repo_dir is not a git clone; updates from the panel stay off" >&2
        return 0
    fi
    if [[ ! "$repo_dir" =~ ^/[A-Za-z0-9._/-]+$ ]]; then
        echo "warning: the path $repo_dir has characters the helper refuses; updates from the panel stay off" >&2
        return 0
    fi
    repo_user="$(stat -c %U "$repo_dir")"
    [ "$(id -u)" -eq 0 ] || as_root=(sudo)

    echo "installing the update helper (talaia-hostd) for the clone in $repo_dir"
    "${as_root[@]}" install -d -m 0755 -o root -g root /var/lib/talaia-host
    # The core runs as uid 65532 and may only write here.
    "${as_root[@]}" install -d -m 0750 -o 65532 -g 65532 /var/lib/talaia-host/requests
    "${as_root[@]}" install -d -m 0700 -o root -g root /var/lib/talaia-hostd
    "${as_root[@]}" mkdir -p /etc/talaia
    "${as_root[@]}" install -m 0755 -o root -g root hostd/talaia-hostd /usr/local/sbin/talaia-hostd
    "${as_root[@]}" install -m 0644 -o root -g root hostd/talaia-update.path hostd/talaia-update.service /etc/systemd/system/
    printf 'REPO_DIR=%s\nREPO_USER=%s\n' "$repo_dir" "$repo_user" |
        "${as_root[@]}" install -m 0644 -o root -g root /dev/stdin /etc/talaia/hostd.conf
    "${as_root[@]}" systemctl daemon-reload
    "${as_root[@]}" systemctl enable --now talaia-update.path
    "${as_root[@]}" systemctl enable talaia-update.service
}

# .env holds the database password: an existing one is never replaced, because
# the database volume was initialised with it.
if [ -f .env ]; then
    echo "keeping the existing .env"
else
    if [ -z "$hostname_arg" ]; then
        hostname_arg="$(hostname -I 2>/dev/null | cut -d' ' -f1 || true)"
        if [ -t 0 ]; then
            hostname_arg="$(ask "Name or address the core is reached at" "$hostname_arg")"
        fi
    fi
    if [ -z "$hostname_arg" ]; then
        echo "error: could not find an address for this machine; pass --hostname" >&2
        exit 1
    fi

    # A port given on the command line must be free; otherwise the first free one is used.
    if [ -n "$port" ] && port_in_use "$port"; then
        echo "error: port $port is already in use; pick another with --port" >&2
        exit 1
    fi
    if [ -n "$agent_port" ] && port_in_use "$agent_port"; then
        echo "error: port $agent_port is already in use; pick another with --agent-port" >&2
        exit 1
    fi
    [ -n "$port" ] || port="$(pick_port 8443 "$agent_port")"
    [ -n "$agent_port" ] || agent_port="$(pick_port 8444 "$port")"

    if [ -z "$acme_provider" ] && [ -t 0 ]; then
        echo "A trusted certificate for the panel needs a domain name whose DNS you can edit through an API."
        if [ "$(ask "Get it automatically with ACME DNS-01 (ovh)? (yes/no)" "no")" = "yes" ]; then
            acme_provider="ovh"
        fi
    fi
    if [ -n "$acme_provider" ]; then
        if [ -z "$acme_email" ] && [ -t 0 ]; then
            acme_email="$(ask "Contact address for the certificate authority (optional)" "")"
        fi
        write_acme_credentials
    fi

    password="$(od -An -tx1 -N32 /dev/urandom | tr -d ' \n')"
    (
        umask 077
        sed -e "s|^POSTGRES_PASSWORD=.*|POSTGRES_PASSWORD=${password}|" \
            -e "s|^TALAIA_PUBLIC_HOSTNAMES=.*|TALAIA_PUBLIC_HOSTNAMES=${hostname_arg}|" \
            -e "s|^TALAIA_PANEL_PORT=.*|TALAIA_PANEL_PORT=${port}|" \
            -e "s|^TALAIA_AGENT_PORT=.*|TALAIA_AGENT_PORT=${agent_port}|" \
            -e "s|^TALAIA_ACME_PROVIDER=.*|TALAIA_ACME_PROVIDER=${acme_provider}|" \
            -e "s|^TALAIA_ACME_EMAIL=.*|TALAIA_ACME_EMAIL=${acme_email}|" \
            .env.example > .env
    )
    echo "wrote .env for $hostname_arg (panel $port, agents $agent_port)"
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

if [ "$install_helper" -eq 1 ]; then
    install_hostd
fi

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
