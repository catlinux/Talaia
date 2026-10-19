#!/usr/bin/env bash
#
# Installs talaia-agent on a Debian or Ubuntu server. Run it as root.
#
# Usage:
#   install.sh --url <core_url> --deb <path-or-https-url> [--token <token>]
#
# Enrollment is not implemented yet: --token is only stored in
# /etc/talaia/enroll.token for a later phase.
set -eu

CONFIG=/etc/talaia/agent.toml
TOKEN_FILE=/etc/talaia/enroll.token

usage() {
    cat >&2 <<'EOF'
Usage: install.sh --url <core_url> --deb <path-or-https-url> [--token <token>]

  --url <core_url>  core URL the agent connects to (required)
  --deb <source>    path to a .deb file or an https:// URL to download (required)
  --token <token>   one-time enrollment token, stored for a later phase (optional)
  -h, --help        show this help
EOF
}

core_url=""
deb_source=""
token=""

while [ "$#" -gt 0 ]; do
    case "$1" in
        --url)
            [ "$#" -ge 2 ] || { echo "error: --url needs a value" >&2; exit 1; }
            core_url="$2"
            shift 2
            ;;
        --deb)
            [ "$#" -ge 2 ] || { echo "error: --deb needs a value" >&2; exit 1; }
            deb_source="$2"
            shift 2
            ;;
        --token)
            [ "$#" -ge 2 ] || { echo "error: --token needs a value" >&2; exit 1; }
            token="$2"
            shift 2
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        *)
            echo "error: unknown option: $1" >&2
            usage
            exit 1
            ;;
    esac
done

if [ "$(id -u)" -ne 0 ]; then
    echo "error: install.sh must be run as root" >&2
    exit 1
fi

if [ -z "$core_url" ]; then
    echo "error: --url is required" >&2
    usage
    exit 1
fi
case "$core_url" in
    http://* | https://*) ;;
    *)
        echo "error: --url must start with http:// or https://" >&2
        exit 1
        ;;
esac

if [ -z "$deb_source" ]; then
    echo "error: --deb is required" >&2
    usage
    exit 1
fi

# The package is only built for Debian and Ubuntu.
if [ ! -r /etc/os-release ]; then
    echo "error: /etc/os-release not found; Debian or Ubuntu is required" >&2
    exit 1
fi
. /etc/os-release
case "${ID:-}" in
    debian | ubuntu) ;;
    *)
        case "${ID_LIKE:-}" in
            *debian*) ;;
            *)
                echo "error: unsupported distribution (${ID:-unknown}); Debian or Ubuntu is required" >&2
                exit 1
                ;;
        esac
        ;;
esac

# The service is managed with systemd; containers without it are not supported.
if ! command -v systemctl >/dev/null 2>&1 || [ ! -d /run/systemd/system ]; then
    echo "error: systemd is required but is not running" >&2
    exit 1
fi

workdir=""
cleanup() {
    if [ -n "$workdir" ]; then
        rm -rf "$workdir"
    fi
}
trap cleanup EXIT

case "$deb_source" in
    https://*)
        if ! command -v curl >/dev/null 2>&1; then
            echo "error: curl is required to download the package" >&2
            exit 1
        fi
        workdir="$(mktemp -d)"
        deb_file="$workdir/talaia-agent.deb"
        echo "downloading $deb_source"
        curl --fail --silent --show-error --location --proto '=https' --tlsv1.2 -o "$deb_file" "$deb_source"
        ;;
    http://*)
        echo "error: only https URLs are allowed for --deb" >&2
        exit 1
        ;;
    *)
        if [ ! -f "$deb_source" ]; then
            echo "error: package not found: $deb_source" >&2
            exit 1
        fi
        # apt-get needs a path that contains a slash to treat it as a file.
        deb_file="$(cd "$(dirname "$deb_source")" && pwd)/$(basename "$deb_source")"
        ;;
esac

echo "installing the talaia-agent package"
apt-get install -y "$deb_file"

# Write core_url into the configuration, replacing any previous value.
mkdir -p /etc/talaia /var/lib/talaia
if [ -f "$CONFIG" ]; then
    grep -v -E '^[[:space:]]*#?[[:space:]]*core_url[[:space:]]*=' "$CONFIG" > "$CONFIG.new" || true
    printf 'core_url = "%s"\n' "$core_url" >> "$CONFIG.new"
    mv "$CONFIG.new" "$CONFIG"
else
    printf 'core_url = "%s"\n' "$core_url" > "$CONFIG"
fi
chmod 0600 "$CONFIG"

# Enrollment arrives in a later phase. The one-time token is only stored here:
# it is never printed, logged or sent anywhere.
if [ -n "$token" ]; then
    (umask 077 && printf '%s\n' "$token" > "$TOKEN_FILE")
    chmod 0600 "$TOKEN_FILE"
fi

systemctl daemon-reload
systemctl enable talaia-agent
systemctl restart talaia-agent

echo "talaia-agent installed and started (configuration: $CONFIG)"
