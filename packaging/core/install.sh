#!/usr/bin/env bash
#
# Sets up talaia-core with Docker Compose on this machine: writes .env with a
# random database password, builds and starts the stack, and creates the
# administrator. Run it from a clone of the repository; it is safe to run again.
#
# Usage:
#   packaging/core/install.sh [--hostname <ip-or-name>] [--port <port>] [--agent-port <port>]
#                             [--acme delegation|ovh] [--public-ip <IPv4>] [--acme-zone <zone>]
#                             [--acme-email <address>] [--no-acme] [--no-acme-check]
#                             [--username <name>] [--no-user] [--no-hostd] [--install-docker]
#
#   --hostname    name or address the core is reached at (asked for in a terminal; default: the first address of this machine)
#   --port        port of the panel (default: the first free one from 8443; 80 and 443 are never chosen on their own)
#   --agent-port  port of the agents (default: the first free one from 8444)
#   --acme        trusted certificate for the panel: delegation (recommended; two DNS records and no
#                 provider key) or ovh (an OVH API key). In a terminal the installer asks and offers
#                 the own CA as the third choice.
#   --public-ip   public IPv4 for the delegation A record (default: the default route source)
#   --acme-zone   DNS zone (the registered domain; default: the last two labels of --hostname)
#   --acme-email  contact address for the certificate authority
#   --no-acme     do not get a trusted certificate now; keep the Talaia CA
#   --no-acme-check  do not check the OVH key with the OVH API (offline installs)
#   --username    name of the administrator (default: admin)
#   --no-user     do not create the administrator
#   --no-hostd    do not install talaia-hostd, the root helper that applies updates requested from the panel
#   --install-docker  install Docker with the official script when it is missing (also offered in a
#                 terminal; it is never installed silently)
#
# Before writing anything it checks the operating system, git, a way to get root, the free disk
# space and Docker with its Compose plugin, and explains the fix for whatever is missing. With a
# DNS name as --hostname it warns when the name does not resolve, and it prints the firewall
# commands for the two ports. It never opens ports.
set -eu

script_path="${BASH_SOURCE[0]}"
case "$script_path" in
    */*) script_dir="${script_path%/*}" ;;
    *) script_dir="." ;;
esac
SCRIPT_DIR="$(cd "$script_dir" && pwd)"

hostname_arg=""
port=""
agent_port=""
acme_provider=""
acme_zone_arg=""
acme_email=""
no_acme_check=0
no_acme=0
public_ip_arg=""
username="admin"
create_user=1
install_helper=1
install_docker=0
env_existed=0
delegation_active=0
delegation_public_ip=""
delegation_host=""
delegation_zone=""
delegation_firewall_done=0
delegation_watched=0
# Set after the preflight; empty until then so helpers can tell.
compose=()

# TALAIA_INSTALL_SOURCE_ONLY=1 sources this file only to reuse its helpers in
# the tests; the installer itself does not run.
if [ "${TALAIA_INSTALL_SOURCE_ONLY:-0}" != "1" ]; then
cd "$SCRIPT_DIR"

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
        --acme-zone)
            [ "$#" -ge 2 ] || { echo "error: --acme-zone needs a value" >&2; exit 1; }
            acme_zone_arg="$2"
            shift 2
            ;;
        --no-acme-check)
            no_acme_check=1
            shift
            ;;
        --no-acme)
            no_acme=1
            shift
            ;;
        --public-ip)
            [ "$#" -ge 2 ] || { echo "error: --public-ip needs a value" >&2; exit 1; }
            public_ip_arg="$2"
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
        --install-docker)
            install_docker=1
            shift
            ;;
        -h | --help)
            sed -n '2,/^set -eu$/p' "$SCRIPT_DIR/${BASH_SOURCE[0]##*/}" |
                sed -e 's/^# \{0,1\}//' -e '/^set -eu$/d'
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
    '' | ovh | delegation) ;;
    *)
        echo "error: unsupported ACME provider $acme_provider (supported: delegation, ovh)" >&2
        exit 1
        ;;
esac
if [ "$no_acme" -eq 1 ] && [ -n "$acme_provider" ]; then
    echo "error: use either --acme or --no-acme, not both" >&2
    exit 1
fi
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

# dns_checked avoids checking the same name twice: the preflight checks the one
# given with --hostname, and the address chosen in a terminal is checked later.
dns_checked=0

# dns_guidance warns when a DNS name of the public hostnames does not resolve or
# resolves to another address. It never fails: a VPS behind provider NAT shows a
# public address that is not one of the machine's own.
dns_guidance() {
    dns_checked=1
    local names name host address local_addresses resolved
    local -A seen
    IFS=',' read -r -a names <<< "$1"
    for name in "${names[@]}"; do
        name="${name//[[:space:]]/}"
        [ -n "$name" ] || continue
        host="${name#[}"
        host="${host%]}"
        case "$host" in
            *:*) continue ;;
        esac
        if [[ "$host" =~ ^[0-9]{1,3}(\.[0-9]{1,3}){3}$ ]]; then
            continue
        fi
        if ! command -v getent >/dev/null 2>&1; then
            echo "warning: cannot check the DNS record of $host (getent is not available)" >&2
            continue
        fi
        local_addresses="$(hostname -I 2>/dev/null || true)"
        resolved=0
        seen=()
        while read -r address _; do
            [ -n "$address" ] || continue
            [ "${seen[$address]:-0}" = "1" ] && continue
            seen[$address]=1
            resolved=1
            if [ -n "$local_addresses" ]; then
                case " $local_addresses " in
                    *" $address "*) ;;
                    *)
                        echo "warning: $host resolves to $address, which is not an address of this machine (${local_addresses}). This is normal behind provider NAT; otherwise check the DNS record." >&2
                        ;;
                esac
            fi
        done <<< "$(getent ahosts "$host" 2>/dev/null || true)"
        if [ "$resolved" -eq 0 ]; then
            echo "warning: $host does not resolve yet. Add a DNS A/AAAA record pointing to this server; browsers will not find the panel until it exists." >&2
        fi
    done
}

# firewall_guidance prints the exact commands for the two ports when ufw or
# firewalld is active. It only reads the status (root or passwordless sudo) and
# never opens a port.
firewall_guidance() {
    local can_root=0 status="" ufw_active=0 firewalld_active=0
    if [ "$(id -u)" -eq 0 ]; then
        can_root=1
    elif sudo -n true 2>/dev/null; then
        can_root=1
    fi

    if command -v ufw >/dev/null 2>&1; then
        if [ "$can_root" -eq 1 ]; then
            if [ "$(id -u)" -eq 0 ]; then
                status="$(ufw status 2>/dev/null || true)"
            else
                status="$(sudo -n ufw status 2>/dev/null || true)"
            fi
            case "$status" in
                *"Status: active"*) ufw_active=1 ;;
            esac
        else
            echo "note: ufw is installed; run 'sudo ufw status' to see whether it is active" >&2
        fi
    fi
    if command -v firewall-cmd >/dev/null 2>&1; then
        if [ "$can_root" -eq 1 ]; then
            if [ "$(id -u)" -eq 0 ]; then
                status="$(firewall-cmd --state 2>/dev/null || true)"
            else
                status="$(sudo -n firewall-cmd --state 2>/dev/null || true)"
            fi
            case "$status" in
                *running*) firewalld_active=1 ;;
            esac
        else
            echo "note: firewalld is installed; run 'sudo firewall-cmd --state' to see whether it is running" >&2
        fi
    fi

    if [ "$ufw_active" -eq 1 ]; then
        echo "ufw is active. Open the two ports with:"
        echo "  sudo ufw allow ${port}/tcp"
        echo "  sudo ufw allow ${agent_port}/tcp"
    elif [ "$firewalld_active" -eq 1 ]; then
        echo "firewalld is active. Open the two ports with:"
        echo "  sudo firewall-cmd --permanent --add-port=${port}/tcp && sudo firewall-cmd --reload"
        echo "  sudo firewall-cmd --permanent --add-port=${agent_port}/tcp && sudo firewall-cmd --reload"
    else
        echo "No active ufw or firewalld was detected."
    fi
    echo "Also open TCP ${port} and ${agent_port} in the firewall or security group of your provider; the installer never opens ports."
}

# preflight checks the machine before anything is written. Every failure names
# the command that fixes it. TALAIA_INSTALL_PREFLIGHT_ONLY=1 stops here, which
# the tests use to exercise the checks without Docker or sudo.
preflight() {
    local os_id="" key value disk_path=/var/lib/docker available_kb=""
    if [ -r /etc/os-release ]; then
        while IFS='=' read -r key value; do
            if [ "$key" = "ID" ]; then
                os_id="${value%\"}"
                os_id="${os_id#\"}"
            fi
        done < /etc/os-release
    fi
    case "$os_id" in
        debian | ubuntu) ;;
        '')
            echo "warning: could not read /etc/os-release; this installer is tested on Debian and Ubuntu" >&2
            ;;
        *)
            echo "warning: $os_id is not Debian or Ubuntu; this installer is tested on those and the next steps may differ" >&2
            ;;
    esac

    if ! command -v git >/dev/null 2>&1; then
        echo "error: git is required to stamp the commit used by updates. Install it with 'sudo apt-get install -y git' (or the equivalent of your distribution) and run again." >&2
        exit 1
    fi

    if ! command -v curl >/dev/null 2>&1; then
        echo "error: curl is required to wait for the core and check it. Install it with 'sudo apt-get install -y curl' (or the equivalent of your distribution) and run again." >&2
        exit 1
    fi

    if [ "$(id -u)" -ne 0 ]; then
        if ! command -v sudo >/dev/null 2>&1; then
            echo "error: this installer needs root or sudo to install the update helper and Docker. Run it as root or install sudo." >&2
            exit 1
        fi
        if ! sudo -n true 2>/dev/null; then
            echo "note: sudo will ask for your password when it is needed"
        fi
    fi

    [ -d "$disk_path" ] || disk_path=/
    while read -r _ _ _ value _; do
        case "$value" in
            '' | *[!0-9]*) continue ;;
        esac
        available_kb="$value"
    done <<< "$(df -Pk "$disk_path" 2>/dev/null || true)"
    case "$available_kb" in
        '' | *[!0-9]*)
            echo "warning: could not read the free disk space of $disk_path" >&2
            ;;
        *)
            if [ "$available_kb" -lt 2097152 ]; then
                echo "error: less than 2 GiB free on $disk_path. Free space (for example: docker system prune -af, or remove old images and builds) and run again." >&2
                exit 1
            fi
            if [ "$available_kb" -lt 5242880 ]; then
                echo "warning: less than 5 GiB free on $disk_path; the first build needs room for images and layers" >&2
            fi
            ;;
    esac

    if [ -n "$hostname_arg" ]; then
        dns_guidance "$hostname_arg"
    fi

    if ! command -v docker >/dev/null 2>&1; then
        echo "Docker is not installed. Talaia runs the core and its database as containers."
        echo "Official installation: curl -fsSL https://get.docker.com | sudo sh"
        install_docker_now() {
            if [ "$(id -u)" -eq 0 ]; then
                curl -fsSL https://get.docker.com | sh
            else
                curl -fsSL https://get.docker.com | sudo sh
            fi
        }
        if [ "$install_docker" -eq 1 ]; then
            echo "installing Docker with the official script"
            if ! install_docker_now; then
                echo "error: Docker installation failed. Install it with 'curl -fsSL https://get.docker.com | sudo sh' and run again." >&2
                exit 1
            fi
        elif [ -t 0 ]; then
            if [ "$(ask "Install Docker now with the official script? (yes/no)" "no")" = "yes" ]; then
                echo "installing Docker with the official script"
                if ! install_docker_now; then
                    echo "error: Docker installation failed. Install it with 'curl -fsSL https://get.docker.com | sudo sh' and run again." >&2
                    exit 1
                fi
            else
                echo "error: Docker is required. Install it with 'curl -fsSL https://get.docker.com | sudo sh' (or pass --install-docker) and run again." >&2
                exit 1
            fi
        else
            echo "error: Docker is required. Install it with 'curl -fsSL https://get.docker.com | sudo sh' (or pass --install-docker) and run again." >&2
            exit 1
        fi
        if ! command -v docker >/dev/null 2>&1; then
            echo "error: Docker is still not available. Open a new shell and run this installer again, or install it with 'curl -fsSL https://get.docker.com | sudo sh'." >&2
            exit 1
        fi
        if ! docker info >/dev/null 2>&1; then
            echo "note: this user cannot talk to Docker yet; the installer will use sudo. Log out and back in to stop using sudo."
        fi
    fi

    if ! docker compose version >/dev/null 2>&1; then
        echo "error: the Docker Compose plugin is missing. Install it with 'sudo apt-get install -y docker-compose-plugin' (on some systems the package is 'docker-compose-v2') and run again." >&2
        exit 1
    fi

    if [ "${TALAIA_INSTALL_PREFLIGHT_ONLY:-0}" = "1" ]; then
        echo "preflight: ok"
        exit 0
    fi
}

# guess_acme_zone prints the last two labels of a host name, the usual registered
# domain. It is only a default: names like .co.uk need the registered domain.
guess_acme_zone() {
    local name="$1" labels
    name="${name%%,*}"
    name="${name#[}"
    name="${name%]}"
    case "$name" in
        *:*) printf '\n'; return 0 ;;
    esac
    case "$name" in
        *[!0-9.]*) : ;;
        *) printf '\n'; return 0 ;;
    esac
    IFS='.' read -r -a labels <<< "$name"
    if [ "${#labels[@]}" -ge 2 ]; then
        printf '%s.%s\n' "${labels[$((${#labels[@]} - 2))]}" "${labels[$((${#labels[@]} - 1))]}"
    else
        printf '%s\n' "$name"
    fi
}

# determine_acme_zone sets acme_zone_value from --acme-zone or the last two
# labels of the public host name, asking in a terminal. The OVH and delegation
# flows share it.
determine_acme_zone() {
    local zone="${acme_zone_arg:-}"
    if [ -z "$zone" ]; then
        zone="$(guess_acme_zone "${TALAIA_PUBLIC_HOSTNAMES%%,*}")"
        if [ -t 0 ]; then
            zone="$(ask "DNS zone (the registered domain; for names like .co.uk type the registered domain)" "$zone")"
        fi
    fi
    if [ -z "$zone" ]; then
        echo "error: pass --acme-zone <zone> (the registered domain) and run again." >&2
        exit 1
    fi
    case "$zone" in
        *[!A-Za-z0-9.-]* | .* | *. | *..*)
            echo "error: --acme-zone may only contain letters, digits, dots and hyphens" >&2
            exit 1
            ;;
    esac
    acme_zone_value="$zone"
}

# read_env_file loads the keys the installer uses from .env without executing
# it. It accepts only known key names and key=value lines.
read_env_file() {
    local line key value
    while IFS= read -r line || [ -n "$line" ]; do
        line="${line%$'\r'}"
        case "$line" in
            '' | '#'*) continue ;;
        esac
        key="${line%%=*}"
        value="${line#*=}"
        case "$key" in
            POSTGRES_PASSWORD | TALAIA_PUBLIC_HOSTNAMES | TALAIA_PANEL_PORT | TALAIA_AGENT_PORT | TALAIA_ACME_PROVIDER | TALAIA_ACME_EMAIL | TALAIA_ACME_DIRECTORY_URL | TALAIA_DNS_ADDRESS | COMPOSE_PROFILES)
                printf -v "$key" '%s' "$value"
                ;;
        esac
    done < .env
}

# read_acme_file reads the four known key=value lines of .env.acme without
# sourcing it, so nothing in it can run.
read_acme_file() {
    local line key value
    acme_endpoint=""
    acme_key=""
    acme_secret=""
    acme_consumer=""
    while IFS= read -r line || [ -n "$line" ]; do
        line="${line%$'\r'}"
        case "$line" in
            '' | '#'*) continue ;;
        esac
        key="${line%%=*}"
        value="${line#*=}"
        case "$key" in
            OVH_ENDPOINT) acme_endpoint="$value" ;;
            OVH_APPLICATION_KEY) acme_key="$value" ;;
            OVH_APPLICATION_SECRET) acme_secret="$value" ;;
            OVH_CONSUMER_KEY) acme_consumer="$value" ;;
        esac
    done < .env.acme
}

# write_acme_file stores the OVH credentials in .env.acme (mode 0600), which
# compose hands to the core.
write_acme_file() {
    (
        umask 077
        printf 'OVH_ENDPOINT=%s\nOVH_APPLICATION_KEY=%s\nOVH_APPLICATION_SECRET=%s\nOVH_CONSUMER_KEY=%s\n' \
            "$acme_endpoint" "$acme_key" "$acme_secret" "$acme_consumer" > .env.acme
    )
    echo "wrote .env.acme"
}

acme_tmp=""
cleanup_acme() {
    [ -z "$acme_tmp" ] || rm -rf "$acme_tmp"
}
# cleanup_on_exit also makes sure a run that stops before production never
# leaves the staging directory configured. The ACME-only test hook is allowed
# to stop after the setup so the tests can inspect the .env it wrote.
cleanup_on_exit() {
    cleanup_acme
    if [ "${TALAIA_INSTALL_ACME_ONLY:-0}" = "1" ]; then
        return 0
    fi
    if [ -f .env ] && grep -q '^TALAIA_ACME_DIRECTORY_URL=.*acme-staging' .env 2>/dev/null; then
        set_env_key TALAIA_ACME_PROVIDER "" 2>/dev/null || true
        remove_env_key TALAIA_ACME_DIRECTORY_URL 2>/dev/null || true
        echo "warning: the run ended before production; ACME is off and staging was removed." >&2
        echo "To finish later: cd $SCRIPT_DIR && ./install.sh --acme delegation" >&2
    fi
}
if [ "${TALAIA_INSTALL_SOURCE_ONLY:-0}" != "1" ]; then
    trap cleanup_on_exit EXIT
fi

# sanitize_ovh_text turns an OVH error into one printable line and removes the
# application secret and the consumer key if OVH echoed them.
sanitize_ovh_text() {
    local text
    text="$(printf '%s' "$1" | tr -c '[:print:]' ' ' | cut -c1-300)"
    text="${text//$acme_secret/[redacted]}"
    text="${text//$acme_consumer/[redacted]}"
    printf '%s\n' "$text"
}

# check_ovh_key asks OVH who the key belongs to and what it may do. It returns
# 0 when the key is valid and has the three rights, 1 when OVH rejected it or a
# right is missing, and 2 when OVH cannot be reached. Secret headers travel in a
# mode 0600 --config file, never in the curl command line.
check_ovh_key() {
    local endpoint="$1" zone="$2" base url timestamp time_response time_code time_body
    local response code body status_value flat rule rule_method rule_path digest config
    local message safe right
    local -a rule_methods=() rule_paths=() missing=()

    if [ -n "$acme_tmp" ]; then
        rm -rf "$acme_tmp"
    fi
    acme_tmp="$(mktemp -d)"
    chmod 700 "$acme_tmp" 2>/dev/null || true

    case "$endpoint" in
        ovh-eu) base="https://eu.api.ovh.com/1.0" ;;
        ovh-ca) base="https://ca.api.ovh.com/1.0" ;;
    esac
    # Test hook: point the OVH calls at a canned server without changing the signature path.
    [ -z "${TALAIA_OVH_API_URL:-}" ] || base="${TALAIA_OVH_API_URL}"
    base="${base%/}"

    if ! command -v sha1sum >/dev/null 2>&1; then
        echo "error: sha1sum is required to sign the OVH request (package coreutils)." >&2
        exit 1
    fi

    if ! time_response="$(curl -sS --max-time 15 -w '\n%{http_code}' "$base/auth/time" 2>"$acme_tmp/curl.err")"; then
        echo "OVH: could not reach OVH at $base" >&2
        return 2
    fi
    time_code="${time_response##*$'\n'}"
    time_body="${time_response%$'\n'*}"
    if [ "$time_code" != "200" ]; then
        echo "OVH: could not reach OVH at $base (HTTP $time_code)" >&2
        return 2
    fi
    timestamp="$(printf '%s' "$time_body" | tr -d '[:space:]')"
    case "$timestamp" in
        '' | *[!0-9]*)
            echo "OVH: OVH returned an invalid time" >&2
            return 1
            ;;
    esac

    url="$base/auth/currentCredential"
    digest="$(printf '%s' "${acme_secret}+${acme_consumer}+GET+${url}++${timestamp}" | sha1sum | cut -d' ' -f1)"
    config="$acme_tmp/headers.conf"
    (
        umask 077
        {
            printf 'header = "X-Ovh-Application: %s"\n' "$acme_key"
            printf 'header = "X-Ovh-Consumer: %s"\n' "$acme_consumer"
            printf 'header = "X-Ovh-Timestamp: %s"\n' "$timestamp"
            printf 'header = "X-Ovh-Signature: $1$%s"\n' "$digest"
        } > "$config"
    )

    if ! response="$(curl -sS --max-time 15 --config "$config" -w '\n%{http_code}' "$url" 2>"$acme_tmp/curl.err")"; then
        echo "OVH: could not reach OVH at $base" >&2
        return 2
    fi
    code="${response##*$'\n'}"
    body="${response%$'\n'*}"

    if [ "$code" = "200" ]; then
        status_value="$(printf '%s' "$body" | tr -d '\r\n' | sed -n 's/.*"status"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
        case "$status_value" in
            validated)
                flat="$(printf '%s' "$body" | tr -d '\r\n\t')"
                while IFS= read -r rule; do
                    rule_method="$(printf '%s' "$rule" | sed -n 's/.*"method"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
                    rule_path="$(printf '%s' "$rule" | sed -n 's/.*"path"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
                    if [ -n "$rule_method" ] && [ -n "$rule_path" ]; then
                        rule_methods+=("$rule_method")
                        rule_paths+=("$rule_path")
                    fi
                done <<< "$(printf '%s' "$flat" | tr '{' '\n')"

                right_covered() {
                    local method="$1" needed="$2" i
                    for i in "${!rule_methods[@]}"; do
                        if [ "${rule_methods[$i]}" = "$method" ] && [[ "$needed" == ${rule_paths[$i]} ]]; then
                            return 0
                        fi
                    done
                    return 1
                }

                if ! right_covered POST "/domain/zone/$zone/record"; then
                    missing+=("POST /domain/zone/$zone/record")
                fi
                if ! right_covered POST "/domain/zone/$zone/refresh"; then
                    missing+=("POST /domain/zone/$zone/refresh")
                fi
                if ! right_covered DELETE "/domain/zone/$zone/record/1"; then
                    missing+=("DELETE /domain/zone/$zone/record/*")
                fi
                if [ "${#missing[@]}" -eq 0 ]; then
                    echo "OVH key: ok"
                    return 0
                fi
                for right in "${missing[@]}"; do
                    echo "OVH key: missing right: $right"
                done
                return 1
                ;;
            '')
                message="$(printf '%s' "$body" | tr -d '\r\n' | sed -n 's/.*"message"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
                [ -n "$message" ] || message="$body"
                safe="$(sanitize_ovh_text "$message")"
                echo "OVH rejected the key: $safe" >&2
                return 1
                ;;
            *)
                echo "OVH: the key was created but it is not validated yet. Open the validation page OVH shows (or the API keys page) and finish the validation, then run the installer again." >&2
                return 1
                ;;
        esac
    fi

    message="$(printf '%s' "$body" | tr -d '\r\n' | sed -n 's/.*"message"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
    [ -n "$message" ] || message="$body"
    safe="$(sanitize_ovh_text "$message")"
    echo "OVH rejected the key (HTTP $code): $safe" >&2
    return 1
}

# setup_acme runs before .env.acme is written and before the build. It prints the
# prepared OVH key link, reads or asks for the credentials, and validates them
# with OVH. With --no-acme-check the network check is skipped (offline installs).
setup_acme() {
    [ "${TALAIA_ACME_PROVIDER:-}" = "ovh" ] || return 0

    local zone="" endpoint="" existing=0
    local create_link guide="https://github.com/catlinux/Talaia/blob/main/docs/en/ovh-dns.md"
    local answer="" key="" secret="" consumer="" result=0

    determine_acme_zone
    zone="$acme_zone_value"

    if [ -f .env.acme ]; then
        existing=1
        read_acme_file
        endpoint="${acme_endpoint:-}"
        if [ -z "$acme_key" ] || [ -z "$acme_secret" ] || [ -z "$acme_consumer" ]; then
            echo "error: .env.acme is incomplete; it needs OVH_APPLICATION_KEY, OVH_APPLICATION_SECRET and OVH_CONSUMER_KEY." >&2
            exit 1
        fi
        case "$acme_key$acme_secret$acme_consumer" in
            *[!A-Za-z0-9._-]*)
                echo "error: .env.acme contains unexpected characters; edit the three OVH values and run again." >&2
                exit 1
                ;;
        esac
    fi
    if [ -z "$endpoint" ]; then
        endpoint="${OVH_ENDPOINT:-}"
    fi
    if [ -z "$endpoint" ] && [ "$existing" -eq 0 ] && [ -t 0 ]; then
        endpoint="$(ask "OVH region (ovh-eu or ovh-ca)" "ovh-eu")"
    fi
    : "${endpoint:=ovh-eu}"
    case "$endpoint" in
        ovh-eu | ovh-ca) ;;
        *)
            echo "error: the OVH region must be ovh-eu or ovh-ca (got $endpoint)" >&2
            exit 1
            ;;
    esac
    acme_endpoint="$endpoint"

    case "$endpoint" in
        ovh-eu)
            create_link="https://api.eu.ovhcloud.com/createToken/?POST=/domain/zone/${zone}/record&POST=/domain/zone/${zone}/refresh&DELETE=/domain/zone/${zone}/record/*"
            ;;
        ovh-ca)
            create_link="https://ca.api.ovh.com/createToken/?POST=/domain/zone/${zone}/record&POST=/domain/zone/${zone}/refresh&DELETE=/domain/zone/${zone}/record/*"
            ;;
    esac
    echo "Create an OVH API key for the zone $zone with these three rights:"
    echo "  $create_link"
    echo "Guide: $guide (also docs/es/ovh-dns.md and docs/ca/ovh-dns.md)."

    if [ "$existing" -eq 0 ]; then
        key="${OVH_APPLICATION_KEY:-}"
        secret="${OVH_APPLICATION_SECRET:-}"
        consumer="${OVH_CONSUMER_KEY:-}"
    fi

    if [ -t 0 ] && [ "$existing" -eq 0 ] && { [ -z "$key" ] || [ -z "$secret" ] || [ -z "$consumer" ]; }; then
        read -r -p "Press Enter when you have the three values: " answer || true
    fi

    while :; do
        if [ "$existing" -eq 0 ]; then
            if [ -t 0 ]; then
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
            if [ -z "$key" ] || [ -z "$secret" ] || [ -z "$consumer" ]; then
                echo "error: set OVH_APPLICATION_KEY, OVH_APPLICATION_SECRET and OVH_CONSUMER_KEY (or run in a terminal)" >&2
                exit 1
            fi
            case "$key$secret$consumer" in
                *[!A-Za-z0-9._-]*)
                    echo "error: the OVH key values contain unexpected characters; copy them again." >&2
                    exit 1
                    ;;
            esac
            acme_key="$key"
            acme_secret="$secret"
            acme_consumer="$consumer"
            acme_endpoint="$endpoint"
        fi

        if [ "$no_acme_check" -eq 1 ]; then
            echo "skipping the OVH check (--no-acme-check)"
            break
        fi

        result=0
        check_ovh_key "$endpoint" "$zone" || result=$?
        if [ "$result" -eq 0 ]; then
            break
        fi

        if [ "$result" -eq 2 ] && [ -t 0 ]; then
            if [ "$(ask "Could not reach OVH. Continue without validating the key? (yes/no)" "no")" = "yes" ]; then
                echo "warning: continuing without the OVH check"
                break
            fi
        fi
        if [ ! -t 0 ]; then
            echo "error: the OVH key could not be validated; fix it or pass --no-acme-check (offline installs)." >&2
            exit 1
        fi
        answer="$(ask "OVH check failed: [1] re-enter the keys, [2] continue without a trusted certificate (own CA, ACME off), [3] abort" "1")"
        case "$answer" in
            1)
                echo "re-enter the three values"
                key=""
                secret=""
                consumer=""
                existing=0
                ;;
            2)
                echo "continuing with the Talaia CA; ACME is off."
                if grep -q '^TALAIA_ACME_PROVIDER=' .env 2>/dev/null; then
                    sed -i 's|^TALAIA_ACME_PROVIDER=.*|TALAIA_ACME_PROVIDER=|' .env
                else
                    printf 'TALAIA_ACME_PROVIDER=\n' >> .env
                fi
                echo "ACME turned off in packaging/core/.env."
                TALAIA_ACME_PROVIDER=""
                acme_state=none
                return 0
                ;;
            3 | *)
                echo "aborting without building anything." >&2
                exit 1
                ;;
        esac
    done

    if [ "$existing" -eq 1 ]; then
        echo "keeping the existing .env.acme"
    else
        write_acme_file
    fi
}

# is_ipv4 returns 0 for a dotted-quad IPv4 address.
is_ipv4() {
    [[ "$1" =~ ^([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})\.([0-9]{1,3})$ ]] || return 1
    local part
    for part in "${BASH_REMATCH[@]:1}"; do
        [ "$((10#$part))" -le 255 ] || return 1
    done
    return 0
}

# is_private_ipv4 covers the ranges the installer must not publish in a DNS A
# record, including the loopback and link-local addresses.
is_private_ipv4() {
    local a b
    IFS=. read -r a b _ <<< "$1"
    [ "$a" = "10" ] && return 0
    [ "$a" = "172" ] && [ "$b" -ge 16 ] && [ "$b" -le 31 ] && return 0
    [ "$a" = "192" ] && [ "$b" = "168" ] && return 0
    [ "$a" = "100" ] && [ "$b" -ge 64 ] && [ "$b" -le 127 ] && return 0
    [ "$a" = "127" ] && return 0
    [ "$a" = "169" ] && [ "$b" = "254" ] && return 0
    return 1
}

# is_dns_name reports whether the first host name is a DNS name, not an IP.
is_dns_name() {
    local name="${1%%,*}"
    name="${name#[}"
    name="${name%]}"
    case "$name" in
        *:*) return 1 ;;
    esac
    is_ipv4 "$name" && return 1
    [[ "$name" == *.* ]] || return 1
    [[ "$name" =~ ^[A-Za-z0-9.-]+$ ]] || return 1
    return 0
}

# detect_public_ipv4 finds the public IPv4 used in the A record: --public-ip,
# the source of the default route (no packet is sent) or the first IPv4 of
# hostname -I. A private address means NAT; it asks in a terminal.
detect_public_ipv4() {
    local ip="" candidate
    if [ -n "$public_ip_arg" ]; then
        ip="$public_ip_arg"
    else
        if command -v ip >/dev/null 2>&1; then
            ip="$(ip -4 route get 1.1.1.1 2>/dev/null | awk '{for (i=1;i<=NF;i++) if ($i=="src") {print $(i+1); exit}}')"
        fi
        if [ -z "$ip" ] && command -v hostname >/dev/null 2>&1; then
            for candidate in $(hostname -I 2>/dev/null || true); do
                if is_ipv4 "$candidate"; then
                    ip="$candidate"
                    break
                fi
            done
        fi
    fi
    if [ -z "$ip" ]; then
        if [ -t 0 ]; then
            ip="$(ask "Public IPv4 address of this server" "")"
        else
            echo "error: could not find the public IPv4 address; pass --public-ip <IPv4>." >&2
            exit 1
        fi
    fi
    while :; do
        if ! is_ipv4 "$ip"; then
            echo "error: $ip is not a valid IPv4 address; pass --public-ip <IPv4>." >&2
            if [ -t 0 ]; then
                ip="$(ask "Public IPv4 address of this server" "")"
                continue
            fi
            exit 1
        fi
        if ! is_private_ipv4 "$ip"; then
            break
        fi
        echo "This server is behind NAT: $ip is a private address." >&2
        if [ -t 0 ]; then
            ip="$(ask "Public IPv4 address of this server" "")"
            continue
        fi
        echo "error: pass the public IPv4 address with --public-ip <IPv4>." >&2
        exit 1
    done
    delegation_public_ip="$ip"
}

# own_acme_dns_running reports whether the acme-dns container of this
# installation is up: a rerun must not take its own server for a conflict.
own_acme_dns_running() {
    [ "${#compose[@]}" -gt 0 ] || return 1
    local id
    id="$("${compose[@]}" --profile acme-dns ps -q --status running acme-dns 2>/dev/null || true)"
    [ -n "$id" ]
}

# check_port_53 returns 0 when port 53 is free for acme-dns on the public IPv4.
# Listeners on 127.x (systemd-resolved) are fine; 0.0.0.0, ::, * or the public
# IPv4 are conflicts. It never changes the firewall.
check_port_53() {
    local can_root=0 lines line addr conflict="" detailed process=""
    port53_conflict=0
    if ! command -v ss >/dev/null 2>&1; then
        echo "warning: ss is not available; cannot check whether port 53 is free." >&2
        return 0
    fi
    lines="$({ ss -lunH 2>/dev/null; ss -ltnH 2>/dev/null; } | grep -E ':(53)([[:space:]]|$)' || true)"
    while IFS= read -r line; do
        [ -n "$line" ] || continue
        addr="$(printf '%s\n' "$line" | grep -oE '[^[:space:]]+:53' | head -n 1)"
        [ -n "$addr" ] || continue
        addr="${addr%:*}"
        case "$addr" in
            127.*) continue ;;
            0.0.0.0 | \[::\] | :: | '*') conflict="$addr" ;;
            "$delegation_public_ip") conflict="$addr" ;;
            *) continue ;;
        esac
        [ -z "$conflict" ] || break
    done <<< "$lines"
    if [ -z "$conflict" ]; then
        return 0
    fi
    if own_acme_dns_running; then
        echo "port 53 is held by the acme-dns container of this installation; keeping it."
        return 0
    fi
    if [ "$(id -u)" -eq 0 ]; then
        can_root=1
    elif sudo -n true 2>/dev/null; then
        can_root=1
    fi
    if [ "$can_root" -eq 1 ]; then
        if [ "$(id -u)" -eq 0 ]; then
            detailed="$(ss -lunpH 2>/dev/null; ss -ltnpH 2>/dev/null)"
        else
            detailed="$(sudo -n ss -lunpH 2>/dev/null; sudo -n ss -ltnpH 2>/dev/null)"
        fi
        process="$(printf '%s\n' "$detailed" | grep -F "$conflict" | sed -n 's/.*users:((\"\([^\"]*\)\".*/\1/p' | head -n 1)"
    fi
    echo "error: something is already listening on port 53 at ${conflict}${process:+ ($process)}." >&2
    echo "acme-dns needs UDP and TCP port 53 on the public IPv4 ${delegation_public_ip}." >&2
    port53_conflict=1
    return 1
}

# validate_env_value rejects the characters that would break the key=value line.
validate_env_value() {
    case "$1" in
        *[[:space:]]* | *\"* | *\'* | *\|* | *\\*) return 1 ;;
    esac
    return 0
}

# set_env_key replaces a key in .env or appends it, atomically and keeping the
# mode at 0600; every other line is kept. Values never contain spaces, quotes,
# newlines or |.
set_env_key() {
    local key="$1" value="$2" tmp_file
    case "$key" in
        '' | *[!A-Za-z0-9_]* | [0-9]*) echo "error: invalid .env key: $key" >&2; return 1 ;;
    esac
    if ! validate_env_value "$value"; then
        echo "error: the value for $key contains a space, quote, newline or |." >&2
        return 1
    fi
    tmp_file="$(mktemp ".env.XXXXXX")" || return 1
    chmod 600 "$tmp_file" 2>/dev/null || true
    if [ -f .env ]; then
        awk -v key="$key" -v value="$value" '
            BEGIN { done = 0 }
            $0 ~ "^" key "=" { print key "=" value; done = 1; next }
            { print }
            END { if (!done) print key "=" value }
        ' .env > "$tmp_file"
    else
        printf '%s=%s\n' "$key" "$value" > "$tmp_file"
    fi
    chmod 600 "$tmp_file" 2>/dev/null || true
    mv "$tmp_file" .env
    chmod 600 .env 2>/dev/null || true
}

# remove_env_key deletes a key from .env, keeping the mode and every other line.
remove_env_key() {
    local key="$1" tmp_file
    [ -f .env ] || return 0
    tmp_file="$(mktemp ".env.XXXXXX")" || return 1
    chmod 600 "$tmp_file" 2>/dev/null || true
    awk -v key="$key" '$0 !~ "^" key "=" { print }' .env > "$tmp_file"
    chmod 600 "$tmp_file" 2>/dev/null || true
    mv "$tmp_file" .env
    chmod 600 .env 2>/dev/null || true
}

# clear_delegation_env removes the acme-dns profile and address when the setup
# switches to OVH or the own CA because port 53 is not available.
clear_delegation_env() {
    remove_env_key COMPOSE_PROFILES
    remove_env_key TALAIA_DNS_ADDRESS
    remove_env_key TALAIA_ACME_DIRECTORY_URL
}

# disable_acme turns the trusted certificate off and removes the staging URL,
# so a run never ends pointing at staging. The panel uses the own CA.
disable_acme() {
    set_env_key TALAIA_ACME_PROVIDER ""
    remove_env_key TALAIA_ACME_DIRECTORY_URL
    # Nothing left to answer: acme-dns must not keep port 53 open for nothing.
    if [ "${delegation_active:-0}" -eq 1 ] && [ "${#compose[@]}" -gt 0 ]; then
        "${compose[@]}" --profile acme-dns rm -sf acme-dns >/dev/null 2>&1 || true
        clear_delegation_env
        delegation_active=0
    fi
    TALAIA_ACME_PROVIDER=""
    acme_state=none
    echo "ACME is off; the panel uses the Talaia CA for now."
    echo "To finish later: cd $SCRIPT_DIR && ./install.sh --acme delegation"
}

# delegation_records prints the two records to create and the AAAA warning.
delegation_records() {
    local host="${delegation_host%.}" zone="$delegation_zone" prefix a_name ns_name
    if [ "$host" = "$zone" ]; then
        prefix=""
        a_name="@"
        ns_name="_acme-challenge"
    else
        prefix="${host%.$zone}"
        if [ "$prefix" = "$host" ]; then
            echo "warning: $host is not inside $zone; check the zone." >&2
            prefix="$host"
        fi
        a_name="$prefix"
        ns_name="_acme-challenge.$prefix"
    fi
    echo
    echo "Create these two records in the DNS zone of $zone (at your DNS provider):"
    echo
    printf '%-5s %-25s %s\n' "Type" "Name" "Value"
    printf '%-5s %-25s %s\n' "A" "$a_name" "$delegation_public_ip"
    printf '%-5s %-25s %s\n' "NS" "$ns_name" "${host}."
    echo
    echo "Do not create an AAAA record for $host: the challenge server answers on IPv4 only."
    echo "Guide: https://github.com/catlinux/Talaia/blob/main/docs/en/dns-delegation.md (also docs/es and docs/ca)."
    echo
}

# delegation_firewall prints the four ports and, only after an explicit yes in a
# terminal with root, opens them in ufw or firewalld. The provider firewall is
# always left to the user.
delegation_firewall() {
    local can_root=0 status="" ufw_active=0 firewalld_active=0 as_root=()
    if [ "$(id -u)" -eq 0 ]; then
        can_root=1
    elif sudo -n true 2>/dev/null; then
        can_root=1
    fi
    [ "$(id -u)" -eq 0 ] || as_root=(sudo)
    if command -v ufw >/dev/null 2>&1; then
        if [ "$can_root" -eq 1 ]; then
            if [ "$(id -u)" -eq 0 ]; then status="$(ufw status 2>/dev/null || true)"; else status="$(sudo -n ufw status 2>/dev/null || true)"; fi
            case "$status" in *"Status: active"*) ufw_active=1 ;; esac
        else
            echo "note: ufw is installed; run 'sudo ufw status' to see whether it is active." >&2
            echo "  sudo ufw allow ${port}/tcp"
            echo "  sudo ufw allow ${agent_port}/tcp"
            echo "  sudo ufw allow 53/udp"
            echo "  sudo ufw allow 53/tcp"
        fi
    fi
    if command -v firewall-cmd >/dev/null 2>&1; then
        if [ "$can_root" -eq 1 ]; then
            if [ "$(id -u)" -eq 0 ]; then status="$(firewall-cmd --state 2>/dev/null || true)"; else status="$(sudo -n firewall-cmd --state 2>/dev/null || true)"; fi
            case "$status" in *running*) firewalld_active=1 ;; esac
        else
            echo "note: firewalld is installed; run 'sudo firewall-cmd --state' to see whether it is running." >&2
            echo "  sudo firewall-cmd --permanent --add-port=${port}/tcp --add-port=${agent_port}/tcp --add-port=53/udp --add-port=53/tcp && sudo firewall-cmd --reload"
        fi
    fi

    echo "The delegation needs these ports open:"
    echo "  TCP $port (panel), TCP $agent_port (agents), UDP 53 and TCP 53 (acme-dns)."
    if [ "$ufw_active" -eq 1 ]; then
        if [ -t 0 ] && [ "$can_root" -eq 1 ] && [ "$(ask "Open these ports in ufw now? (yes/no)" "no")" = "yes" ]; then
            "${as_root[@]}" ufw allow "${port}/tcp"
            "${as_root[@]}" ufw allow "${agent_port}/tcp"
            "${as_root[@]}" ufw allow 53/udp
            "${as_root[@]}" ufw allow 53/tcp
            echo "opened the ports in ufw"
        else
            echo "ufw is active. Open them with:"
            echo "  sudo ufw allow ${port}/tcp"
            echo "  sudo ufw allow ${agent_port}/tcp"
            echo "  sudo ufw allow 53/udp"
            echo "  sudo ufw allow 53/tcp"
        fi
    elif [ "$firewalld_active" -eq 1 ]; then
        if [ -t 0 ] && [ "$can_root" -eq 1 ] && [ "$(ask "Open these ports in firewalld now? (yes/no)" "no")" = "yes" ]; then
            "${as_root[@]}" firewall-cmd --permanent --add-port="${port}/tcp"
            "${as_root[@]}" firewall-cmd --permanent --add-port="${agent_port}/tcp"
            "${as_root[@]}" firewall-cmd --permanent --add-port=53/udp
            "${as_root[@]}" firewall-cmd --permanent --add-port=53/tcp
            "${as_root[@]}" firewall-cmd --reload
            echo "opened the ports in firewalld"
        else
            echo "firewalld is active. Open them with:"
            echo "  sudo firewall-cmd --permanent --add-port=${port}/tcp --add-port=${agent_port}/tcp --add-port=53/udp --add-port=53/tcp && sudo firewall-cmd --reload"
        fi
    else
        if command -v ufw >/dev/null 2>&1 || command -v firewall-cmd >/dev/null 2>&1; then
            echo "ufw or firewalld is installed but not active."
        else
            echo "No active ufw or firewalld was detected."
        fi
    fi
    echo "Also allow TCP ${port}, TCP ${agent_port}, UDP 53 and TCP 53 in the firewall or security group of your provider."
    echo "The installer never opens a port in the provider firewall."
    if [ -t 0 ]; then
        read -r -p "Press Enter when the records exist and the ports are open" _ || true
    fi
    delegation_firewall_done=1
}

# setup_delegation prepares the acme-dns delegation before the build: public IP,
# port 53, .env keys, records and firewall. It may switch to OVH or ACME off.
setup_delegation() {
    local host choice=""
    if ! is_dns_name "${TALAIA_PUBLIC_HOSTNAMES%%,*}"; then
        echo "error: the delegation needs a DNS name in --hostname (not an IP address)." >&2
        echo "Use --acme ovh or --no-acme, or install with --hostname talaia.example.com." >&2
        exit 1
    fi
    host="${TALAIA_PUBLIC_HOSTNAMES%%,*}"
    delegation_host="${host%.}"
    detect_public_ipv4
    if ! check_port_53; then
        if [ -t 0 ]; then
            echo "Port 53 is already in use, so acme-dns cannot bind the public IPv4."
            echo "  1) use an OVH API key instead"
            echo "  2) continue without a trusted certificate for now (Talaia's own CA)"
            echo "  3) quit"
            choice="$(ask "Choice" "2")"
            case "$choice" in
                1)
                    clear_delegation_env
                    set_env_key TALAIA_ACME_PROVIDER ovh
                    acme_provider="ovh"
                    TALAIA_ACME_PROVIDER="ovh"
                    delegation_active=0
                    return 0
                    ;;
                2)
                    clear_delegation_env
                    disable_acme
                    delegation_active=0
                    return 0
                    ;;
                *)
                    clear_delegation_env
                    disable_acme
                    exit 1
                    ;;
            esac
        fi
        echo "error: stop the process listening on port 53, or use --acme ovh or --no-acme." >&2
        exit 1
    fi
    determine_acme_zone
    delegation_zone="$acme_zone_value"
    set_env_key COMPOSE_PROFILES acme-dns
    set_env_key TALAIA_DNS_ADDRESS "$delegation_public_ip"
    set_env_key TALAIA_ACME_DIRECTORY_URL "https://acme-staging-v02.api.letsencrypt.org/directory"
    set_env_key TALAIA_ACME_PROVIDER delegation
    TALAIA_ACME_PROVIDER="delegation"
    acme_provider="delegation"
    delegation_active=1
    delegation_records
    delegation_firewall
}

# delegation_check runs talaia-core acme check-delegation until it passes. In a
# terminal it waits for Enter, o (own CA) or q; without one it retries 10 times.
delegation_check() {
    local attempt=1 max=10 choice=""
    while :; do
        echo "checking the delegation with the public resolvers (attempt $attempt of $max)"
        if "${compose[@]}" run --rm --no-deps core acme check-delegation --hostname "$delegation_host" --address "$delegation_public_ip"; then
            return 0
        fi
        if [ -t 0 ]; then
            echo "DNS changes can take a few minutes."
            choice="$(ask "Enter: check again, o: continue without a trusted certificate, q: quit" "")"
            case "$choice" in
                '') : ;;
                o | O)
                    disable_acme
                    return 1
                    ;;
                q | Q)
                    disable_acme
                    "${compose[@]}" up -d
                    exit 1
                    ;;
                *) echo "unknown choice; checking again" >&2 ;;
            esac
        else
            if [ "$attempt" -ge "$max" ]; then
                echo "error: the delegation check did not pass after $max attempts." >&2
                disable_acme
                return 1
            fi
            attempt=$((attempt + 1))
            sleep 30
        fi
    done
}

# delegation_staging restarts the core with staging, waits for the staging
# certificate, then switches to production and waits for the real one. It always
# ends with staging removed (or with the trusted certificate finished).
delegation_staging() {
    local restart_at log warning="" i choice production_at
    echo "restarting the core to ask for the staging certificate"
    "${compose[@]}" restart core
    restart_at="$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || true)"
    for i in $(seq 1 48); do
        log="$("${compose[@]}" logs --since "$restart_at" core 2>/dev/null || true)"
        if printf '%s\n' "$log" | grep -qE 'ACME certificate ready|using the stored ACME certificate'; then
            echo "staging certificate: ok (the delegation works from the internet)"
            set_env_key TALAIA_ACME_DIRECTORY_URL ""
            production_at="$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || true)"
            "${compose[@]}" up -d core
            acme_state=pending
            acme_watch "$production_at"
            delegation_watched=1
            return 0
        fi
        warning="$(printf '%s\n' "$log" | grep -Ei 'acme|renewal failed' | grep -E '"level":"(WARN|ERROR)"' | tail -n 1 || true)"
        sleep 5
    done
    echo "error: the staging certificate did not arrive within four minutes." >&2
    if [ -n "$warning" ]; then
        echo "last ACME warning: $warning" >&2
    fi
    if [ -t 0 ]; then
        choice="$(ask "Staging failed: [1] retry, [2] continue without a trusted certificate, [3] quit" "1")"
        case "$choice" in
            1) delegation_staging; return $? ;;
            2)
                disable_acme
                "${compose[@]}" up -d
                return 1
                ;;
            *)
                disable_acme
                "${compose[@]}" up -d
                exit 1
                ;;
        esac
    fi
    disable_acme
    "${compose[@]}" up -d
    return 1
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

# self_check asks the two TLS listeners of the core from this machine. It says
# ok or FAILED and never fails the installation by itself.
self_check() {
    if curl -fsk -o /dev/null "https://127.0.0.1:${port}/healthz" 2>/dev/null; then
        echo "panel: ok (https://127.0.0.1:${port}/healthz)"
    else
        echo "panel: FAILED (https://127.0.0.1:${port}/healthz); see: ${compose[*]} logs core"
    fi
    if curl -fsk -o /dev/null "https://127.0.0.1:${agent_port}/ca.pem" 2>/dev/null; then
        echo "agents: ok (https://127.0.0.1:${agent_port}/ca.pem)"
    else
        echo "agents: FAILED (https://127.0.0.1:${agent_port}/ca.pem); see: ${compose[*]} logs core"
    fi
}

# acme_watch waits up to three minutes for the certificate. It only reads the
# core log (which never holds secrets) and never blocks the rest of the output
# when the wait times out. An optional argument limits the log to logs after
# that RFC 3339 time; the delegation flow uses it to ignore the staging success.
acme_watch() {
    local since="${1:-}" deadline log warning="" now
    now="$(date +%s 2>/dev/null || echo 0)"
    deadline=$((now + 180))
    while [ "$(date +%s 2>/dev/null || echo 0)" -lt "$deadline" ]; do
        if [ -n "$since" ]; then
            log="$("${compose[@]}" logs --since "$since" core 2>/dev/null || true)"
        else
            # The whole log, not only this run: a rerun that changes nothing does
            # not restart the core, and the certificate it obtained earlier is valid.
            log="$("${compose[@]}" logs core 2>/dev/null || true)"
        fi
        if printf '%s\n' "$log" | grep -qE 'ACME certificate ready|using the stored ACME certificate'; then
            acme_state=ready
            echo "ACME certificate: obtained"
            return 0
        fi
        if [ -z "$since" ]; then
            log="$("${compose[@]}" logs --since "$run_started" core 2>/dev/null || true)"
        fi
        warning="$(printf '%s\n' "$log" | grep -Ei 'acme|renewal failed' | grep -E '"level":"(WARN|ERROR)"|renewal failed' | tail -n 1 || true)"
        sleep 5
    done
    if [ -n "$warning" ]; then
        echo "ACME certificate: still pending; last warning: $warning"
    else
        echo "ACME certificate: still pending (the core keeps trying; see: ${compose[*]} logs core | grep ACME)"
    fi
}

# The tests source this file with TALAIA_INSTALL_SOURCE_ONLY=1 to call the
# helpers; only the main run executes preflight and the installation.
if [ "${TALAIA_INSTALL_SOURCE_ONLY:-0}" = "1" ]; then
    return 0 2>/dev/null || exit 0
fi

preflight

# The commit is stamped into the core so the panel can tell which updates are new.
git_commit="$(git -C "$SCRIPT_DIR" rev-parse HEAD 2>/dev/null || true)"

# Use sudo only when the current user cannot talk to the Docker daemon.
if docker info >/dev/null 2>&1; then
    compose=(env "TALAIA_GIT_COMMIT=${git_commit}" docker compose)
else
    compose=(sudo env "TALAIA_GIT_COMMIT=${git_commit}" docker compose)
fi

# .env holds the database password: an existing one is never replaced, because
# the database volume was initialised with it.
if [ -f .env ]; then
    env_existed=1
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

    if [ -z "$acme_provider" ] && [ "$no_acme" -eq 0 ] && [ -t 0 ] && is_dns_name "$hostname_arg"; then
        echo "Which certificate should the panel use?"
        echo "  1) automatic, with two DNS records (recommended)"
        echo "  2) with an OVH API key"
        echo "  3) none for now (Talaia's own CA)"
        while :; do
            acme_choice="$(ask "Choice" "1")"
            case "$acme_choice" in
                1) acme_provider="delegation"; break ;;
                2) acme_provider="ovh"; break ;;
                3) acme_provider=""; break ;;
                *) echo "error: choose 1, 2 or 3" >&2 ;;
            esac
        done
    fi
    if [ -n "$acme_provider" ]; then
        if [ -z "$acme_email" ] && [ -t 0 ]; then
            acme_email="$(ask "Contact address for the certificate authority (optional)" "")"
        fi
    fi

    # Both go into .env unquoted and through sed: only plain characters.
    hostname_pattern='^[][A-Za-z0-9.:,-]+$'
    if [[ ! "$hostname_arg" =~ $hostname_pattern ]]; then
        echo "error: the host name may only contain letters, digits, dots, colons, hyphens, commas and brackets" >&2
        exit 1
    fi
    email_pattern='^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+$'
    if [ -n "$acme_email" ] && [[ ! "$acme_email" =~ $email_pattern ]]; then
        echo "error: --acme-email is not a valid address" >&2
        exit 1
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

# Load the values the installer wrote, without executing .env as a script.
read_env_file
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

if [ "$env_existed" -eq 1 ] && [ "$acme_provider" = "ovh" ] && [ "${TALAIA_ACME_PROVIDER:-}" != "ovh" ]; then
    echo "error: packaging/core/.env already exists and does not enable ACME. Set TALAIA_ACME_PROVIDER=ovh in it and run again (the installer never replaces an existing .env)." >&2
    exit 1
fi
if [ "$no_acme" -eq 1 ]; then
    disable_acme
elif [ "$acme_provider" = "delegation" ]; then
    setup_delegation
elif [ -z "$acme_provider" ]; then
    case "${TALAIA_ACME_PROVIDER:-}" in
        delegation)
            if [ "$env_existed" -eq 0 ] || [ -n "${TALAIA_ACME_DIRECTORY_URL:-}" ]; then
                setup_delegation
            fi
            ;;
    esac
fi
if [ "${TALAIA_ACME_PROVIDER:-}" = "ovh" ]; then
    setup_acme
fi

# The tests stop after the ACME check; this is not an option for users.
if [ "${TALAIA_INSTALL_ACME_ONLY:-0}" = "1" ]; then
    echo "acme: checked"
    exit 0
fi

if [ "$dns_checked" -eq 0 ] && [ -n "${TALAIA_PUBLIC_HOSTNAMES:-}" ]; then
    dns_guidance "$TALAIA_PUBLIC_HOSTNAMES"
fi
if [ "$delegation_firewall_done" -eq 0 ]; then
    firewall_guidance
fi

helper_active=0
if [ "$install_helper" -eq 1 ]; then
    install_hostd
    if command -v systemctl >/dev/null 2>&1 && [ -d /run/systemd/system ]; then
        helper_state="$(systemctl is-active talaia-update.path 2>/dev/null || true)"
        if [ "$helper_state" = "active" ]; then
            helper_active=1
            echo "update helper: active"
        else
            echo "update helper: not active (${helper_state:-unknown}); see: sudo systemctl status talaia-update.path"
        fi
    else
        echo "update helper: not active (systemd is not running); updates from the panel stay off"
    fi
else
    echo "update helper: not installed (--no-hostd); updates from the panel stay off"
fi

run_started="$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || true)"

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

self_check

if [ "$create_user" -eq 1 ]; then
    users="$("${compose[@]}" exec -T db psql -U talaia -d talaia -tAc 'SELECT count(*) FROM app_user' 2>/dev/null | tr -d '[:space:]' || true)"
    if [ "$users" = "0" ]; then
        echo "creating the administrator \"$username\" (the password needs at least 12 characters)"
        "${compose[@]}" run --rm core user create --username "$username"
    else
        echo "an administrator already exists; not creating another"
    fi
fi

if [ "$delegation_active" -eq 1 ]; then
    if delegation_check; then
        # A failed staging turns ACME off and returns 1; the summary below must
        # still be printed, so set -e must not stop here.
        delegation_staging || true
    else
        echo "continuing with the Talaia CA for now."
        "${compose[@]}" up -d
    fi
fi

if [ "${delegation_watched:-0}" != "1" ]; then
    acme_state=none
    if [ -n "${TALAIA_ACME_PROVIDER:-}" ]; then
        acme_state=pending
        acme_watch
    fi
fi

echo
echo "talaia-core is running."
echo
echo "  Panel: $panel_url"
echo "  Agents: $agent_url"
case "$acme_state" in
    ready)
        echo "  Certificate: trusted by browsers (ACME)."
        ;;
    pending)
        echo "  Certificate: ACME is configured but not ready yet; until it is, the browser warns about the Talaia CA."
        echo "  Check: cd $SCRIPT_DIR && ${compose[*]} logs core | grep ACME"
        ;;
    *)
        echo "  The browser warns about the certificate until you trust the Talaia CA:"
        echo "  download ${agent_url}/ca.pem and install it as a trusted authority."
        ;;
esac
echo
echo "To add a server, create a token here and run the command it prints on that server:"
echo
echo "  cd $SCRIPT_DIR && ${compose[*]} run --rm core token create --name <server-name>"
echo
if [ "$install_helper" -eq 1 ] && [ "$helper_active" -eq 1 ]; then
    echo "To update Talaia, open Settings in the panel; the update helper applies it."
else
    echo "To update Talaia, open Settings in the panel (the update helper is not active; see above)."
fi
echo "Logs: cd $SCRIPT_DIR && ${compose[*]} logs -f core"
