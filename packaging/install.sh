#!/usr/bin/env bash
#
# Installs talaia-agent on a Debian or Ubuntu server. Run it as root.
#
# Usage:
#   install.sh --url <https-core-url> --deb <path-or-https-url> --token <token> --ca-fingerprint <sha256-hex>
#
# The agent enrolls on its first start: it fetches the core CA, checks it against
# --ca-fingerprint (the trust anchor), and exchanges the one-time token for its
# own client certificate. The token file is deleted after a successful enrollment.
set -eu

CONFIG=/etc/talaia/agent.toml
TOKEN_FILE=/etc/talaia/enroll.token

usage() {
    cat >&2 <<'EOF'
Usage: install.sh --url <https-core-url> --deb <path-or-https-url> --token <token> --ca-fingerprint <sha256-hex>

  --url <core_url>  core URL the agent connects to (required)
  --deb <source>    path to a .deb file or an https:// URL to download (required)
  --token <token>   one-time enrollment token (from "talaia-core token create")
  --ca-fingerprint <hex>  SHA-256 fingerprint of the core CA (printed with the token)
  -h, --help        show this help
EOF
}

core_url=""
deb_source=""
token=""
ca_fingerprint=""

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
        --ca-fingerprint)
            [ "$#" -ge 2 ] || { echo "error: --ca-fingerprint needs a value" >&2; exit 1; }
            ca_fingerprint="$2"
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
    https://*) ;;
    *)
        echo "error: --url must start with https://" >&2
        exit 1
        ;;
esac

if [ -z "$token" ] || [ -z "$ca_fingerprint" ]; then
    echo "error: --token and --ca-fingerprint are required" >&2
    usage
    exit 1
fi
case "$ca_fingerprint" in
    *[!0-9a-fA-F]* | "")
        echo "error: --ca-fingerprint must be hexadecimal" >&2
        exit 1
        ;;
esac
if [ "${#ca_fingerprint}" -ne 64 ]; then
    echo "error: --ca-fingerprint must have 64 hexadecimal characters" >&2
    exit 1
fi

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

# Write core_url and ca_fingerprint into the configuration, replacing previous values.
mkdir -p /etc/talaia /var/lib/talaia
if [ -f "$CONFIG" ]; then
    grep -v -E '^[[:space:]]*#?[[:space:]]*(core_url|ca_fingerprint)[[:space:]]*=' "$CONFIG" > "$CONFIG.new" || true
else
    : > "$CONFIG.new"
fi
printf 'core_url = "%s"
' "$core_url" >> "$CONFIG.new"
printf 'ca_fingerprint = "%s"
' "$ca_fingerprint" >> "$CONFIG.new"
mv "$CONFIG.new" "$CONFIG"
chmod 0600 "$CONFIG"

# The one-time token is read by the agent on its first start and deleted after
# a successful enrollment. It is never printed, logged or sent anywhere else.
(umask 077 && printf '%s
' "$token" > "$TOKEN_FILE")
chmod 0600 "$TOKEN_FILE"

systemctl daemon-reload
systemctl enable talaia-agent
systemctl restart talaia-agent

echo "talaia-agent installed and started (configuration: $CONFIG)"
