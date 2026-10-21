#!/usr/bin/env bash
#
# Tests the installer with stub commands: the preflight, the guided OVH check
# and the bootstrap. It uses no real network, sudo or Docker and writes only in
# a temporary directory. It runs on Debian, Ubuntu and Git Bash on Windows.
set -eu

script_path="${BASH_SOURCE[0]}"
case "$script_path" in
    */*) script_dir="${script_path%/*}" ;;
    *) script_dir="." ;;
esac
script_dir="$(cd "$script_dir" && pwd)"
repo_dir="$(cd "$script_dir/../.." && pwd)"
installer="$script_dir/install.sh"
bootstrap="$repo_dir/install-core.sh"
bash_path="$(command -v bash)"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# The installer is copied so .env, if any, cannot land in the repository.
cp "$installer" "$tmp/install.sh"
cp "$script_dir/.env.example" "$tmp/.env.example"

stub="$tmp/bin"
mkdir -p "$stub"
system_path="$PATH"
failures=0

fail() {
    echo "FAIL: $*" >&2
    failures=$((failures + 1))
}

pass() {
    echo "ok: $*"
}

write_stub() {
    local name="$1"
    cat > "$stub/$name"
    chmod +x "$stub/$name"
}

write_stub git <<'EOF'
#!/bin/sh
exit 0
EOF

write_stub id <<'EOF'
#!/bin/sh
echo 1000
EOF

write_stub sudo <<'EOF'
#!/bin/sh
exit 0
EOF

write_stub df <<'EOF'
#!/bin/sh
printf 'Filesystem 1024-blocks Used Available Capacity Mounted on\n'
printf '/dev/fake 100000000 1000 %s 1%% /\n' "${DF_AVAILABLE:-10000000}"
EOF

write_stub getent <<'EOF'
#!/bin/sh
exit 1
EOF

write_stub hostname <<'EOF'
#!/bin/sh
printf '%s\n' "${HOSTNAME_OUT:-192.0.2.10}"
EOF

write_stub ip <<'EOF'
#!/bin/sh
if [ -n "${IP_ROUTE_OUTPUT:-}" ]; then
    printf '%s\n' "$IP_ROUTE_OUTPUT"
    exit "${IP_ROUTE_STATUS:-0}"
fi
exit 1
EOF

write_stub ss <<'EOF'
#!/bin/sh
case "$*" in
    *"sport = :"*) exit 0 ;;
esac
if [ -n "${SS_OUTPUT:-}" ]; then
    printf '%s\n' "$SS_OUTPUT"
fi
exit 0
EOF

write_stub sleep <<'EOF'
#!/bin/sh
exit 0
EOF

write_stub curl <<'EOF'
#!/bin/sh
if [ -n "${CURL_LOG:-}" ]; then
    printf '%s\n' "$*" >> "$CURL_LOG"
fi
if [ "${CURL_CREATE_DOCKER:-0}" = "1" ]; then
    printf '#!/bin/sh\nexit 0\n' > "$STUB_DIR/docker"
    PATH="$SYSTEM_PATH" chmod +x "$STUB_DIR/docker"
    exit 0
fi
config=""
prev=""
url=""
for arg in "$@"; do
    if [ "$prev" = "--config" ]; then
        config="$arg"
    fi
    case "$arg" in
        http://*|https://*) url="$arg" ;;
    esac
    prev="$arg"
done
case "$url" in
    */auth/time)
        printf '%s\n200\n' "${OVH_TIME:-1700000000}"
        exit 0
        ;;
esac
if [ -n "$config" ] && [ -n "${CURL_CONFIG_LOG:-}" ]; then
    cat "$config" >> "$CURL_CONFIG_LOG"
fi
case "${OVH_CASE:-valid}" in
    valid)
        printf '%s\n200\n' '{"status":"validated","rules":[{"method":"POST","path":"/domain/zone/example.com/record"},{"method":"POST","path":"/domain/zone/example.com/refresh"},{"method":"DELETE","path":"/domain/zone/example.com/record/*"}]}'
        ;;
    missing)
        printf '%s\n200\n' '{"status":"validated","rules":[{"method":"POST","path":"/domain/zone/example.com/record"},{"method":"POST","path":"/domain/zone/example.com/refresh"}]}'
        ;;
    forbidden)
        printf '%s\n403\n' '{"message":"This service is not available"}'
        ;;
    pending)
        printf '%s\n200\n' '{"status":"pendingValidation","rules":[]}'
        ;;
    unreachable)
        exit 7
        ;;
    *)
        printf '%s\n500\n' '{"message":"unexpected"}'
        ;;
esac
EOF

write_stub docker <<'EOF'
#!/bin/sh
exit 0
EOF

# Source the installer's helpers without running the installation. The variable
# is not exported, so the child runs below start the installer normally.
TALAIA_INSTALL_SOURCE_ONLY=1
. "$installer"
unset TALAIA_INSTALL_SOURCE_ONLY

# run_install runs the copied installer with only the stubs in its PATH.
run_install() {
    local status=0
    PATH="$stub" \
        TALAIA_INSTALL_PREFLIGHT_ONLY=1 \
        SYSTEM_PATH="$system_path" \
        STUB_DIR="$stub" \
        DF_AVAILABLE="${DF_AVAILABLE:-10000000}" \
        CURL_LOG="${CURL_LOG:-}" \
        CURL_CREATE_DOCKER="${CURL_CREATE_DOCKER:-0}" \
        "$bash_path" "$tmp/install.sh" "$@" < /dev/null >"$tmp/out" 2>"$tmp/err" || status=$?
    return "$status"
}

# 1. Docker is missing: it stops and names the official installation command.
rm -f "$stub/docker"
if run_install --hostname 127.0.0.1 --no-user; then
    fail "the installer continued without Docker"
else
    pass "Docker missing stops the installer"
fi
if grep -q 'curl -fsSL https://get.docker.com | sudo sh' "$tmp/err"; then
    pass "Docker missing names the official command"
else
    fail "the official Docker command is not in stderr"
fi
if [ -e "$tmp/.env" ]; then
    fail "the preflight wrote .env"
else
    pass "the preflight writes nothing"
fi

# 2. --install-docker runs the official command (stubbed) and never silently.
rm -f "$stub/docker"
curl_log="$tmp/curl.log"
: > "$curl_log"
if CURL_LOG="$curl_log" CURL_CREATE_DOCKER=1 run_install --hostname 127.0.0.1 --no-user --install-docker; then
    pass "--install-docker completes the preflight"
else
    fail "--install-docker did not complete the preflight"
fi
if grep -q 'get.docker.com' "$curl_log"; then
    pass "--install-docker asked for the official script"
else
    fail "--install-docker did not call the official script"
fi
if grep -q 'preflight: ok' "$tmp/out"; then
    pass "the successful preflight reports ok"
else
    fail "the successful preflight did not report ok"
fi

# 3. Less than 2 GiB free stops with the fix.
if DF_AVAILABLE=1000000 run_install --hostname 127.0.0.1 --no-user; then
    fail "the installer continued with a low disk"
else
    pass "a low disk stops the installer"
fi
if grep -q '2 GiB' "$tmp/err"; then
    pass "a low disk names the space it needs"
else
    fail "the low-disk error is missing"
fi

# 4. git missing stops before anything else.
mv "$stub/git" "$tmp/git.keep"
if run_install --hostname 127.0.0.1 --no-user; then
    fail "the installer continued without git"
else
    pass "git missing stops the installer"
fi
if grep -q 'git is required' "$tmp/err"; then
    pass "git missing names the fix"
else
    fail "the git error is missing"
fi
mv "$tmp/git.keep" "$stub/git"

# 5. A DNS name that does not resolve warns, then continues to the Docker check.
rm -f "$stub/docker"
if run_install --hostname core.example.com --no-user; then
    fail "the installer continued without Docker"
else
    pass "a name that does not resolve does not stop the DNS check"
fi
if grep -q 'does not resolve' "$tmp/err"; then
    pass "an unresolved name is reported"
else
    fail "the unresolved-name warning is missing"
fi
if grep -q 'Docker is required' "$tmp/err"; then
    pass "the installer continues to the Docker check"
else
    fail "the installer did not reach the Docker check"
fi

# 6. With everything present the preflight passes.
write_stub docker <<'EOF'
#!/bin/sh
exit 0
EOF
if run_install --hostname 127.0.0.1 --no-user; then
    pass "a complete machine passes the preflight"
else
    fail "a complete machine did not pass the preflight"
fi

# The ACME checks use an existing .env so the installer reaches the OVH step
# without writing .env, and TALAIA_INSTALL_ACME_ONLY=1 stops before the build.
acme_url="https://ovh.test/1.0"
ovh_time=1700000000
ovh_key="appkeyvalue"
ovh_secret="secretvalue"
ovh_consumer="consumerkeyvalue"
expected_sig="$(printf '%s' "${ovh_secret}+${ovh_consumer}+GET+${acme_url}/auth/currentCredential++${ovh_time}" | sha1sum | cut -d' ' -f1)"

write_acme_env() {
    cat > "$tmp/.env" <<'EOF'
POSTGRES_PASSWORD=test
TALAIA_PUBLIC_HOSTNAMES=talaia.example.com
TALAIA_PANEL_PORT=8443
TALAIA_AGENT_PORT=8444
TALAIA_ACME_PROVIDER=ovh
TALAIA_ACME_EMAIL=
EOF
}

run_acme() {
    local status=0
    PATH="$stub:$system_path" \
        TALAIA_INSTALL_ACME_ONLY=1 \
        SYSTEM_PATH="$system_path" \
        STUB_DIR="$stub" \
        OVH_APPLICATION_KEY="$ovh_key" \
        OVH_APPLICATION_SECRET="$ovh_secret" \
        OVH_CONSUMER_KEY="$ovh_consumer" \
        OVH_ENDPOINT="${ACME_ENDPOINT:-ovh-eu}" \
        TALAIA_OVH_API_URL="$acme_url" \
        OVH_TIME="$ovh_time" \
        OVH_CASE="${OVH_CASE:-valid}" \
        CURL_LOG="${CURL_LOG:-}" \
        CURL_CONFIG_LOG="${CURL_CONFIG_LOG:-}" \
        "$bash_path" "$tmp/install.sh" "$@" < /dev/null >"$tmp/out" 2>"$tmp/err" || status=$?
    return "$status"
}

# 7. A valid key: expected signature, no secret on the command line, link.
write_acme_env
rm -f "$tmp/.env.acme"
curl_log="$tmp/acme-curl.log"
config_log="$tmp/acme-config.log"
: > "$curl_log"
: > "$config_log"
if CURL_LOG="$curl_log" CURL_CONFIG_LOG="$config_log" run_acme; then
    pass "a valid OVH key passes the check"
else
    fail "a valid OVH key did not pass the check"
fi
if grep -q 'OVH key: ok' "$tmp/out"; then
    pass "the valid key says ok"
else
    fail "the valid key does not say ok"
fi
if grep -q 'api.eu.ovhcloud.com/createToken' "$tmp/out"; then
    pass "the EU key link is printed"
else
    fail "the EU key link is missing"
fi
if grep -q 'POST=/domain/zone/example.com/record' "$tmp/out"; then
    pass "the zone is guessed from the host name"
else
    fail "the guessed zone is wrong"
fi
if grep -q 'docs/en/ovh-dns.md' "$tmp/out"; then
    pass "the OVH guide is linked"
else
    fail "the OVH guide link is missing"
fi
if grep -qF "X-Ovh-Signature: \$1\$$expected_sig" "$config_log"; then
    pass "the OVH signature matches the fixed timestamp"
else
    fail "the OVH signature is wrong"
fi
if grep -qF '$1$' "$curl_log"; then
    fail "the signature appears on the curl command line"
else
    pass "the signature stays out of the curl command line"
fi
if grep -qF "$ovh_secret" "$tmp/out" || grep -qF "$ovh_secret" "$tmp/err"; then
    fail "the application secret appears in the output"
else
    pass "the application secret is not printed"
fi
if grep -qF "$ovh_consumer" "$tmp/out" || grep -qF "$ovh_consumer" "$tmp/err"; then
    fail "the consumer key appears in the output"
else
    pass "the consumer key is not printed"
fi
if [ -f "$tmp/.env.acme" ] && grep -q "OVH_APPLICATION_KEY=$ovh_key" "$tmp/.env.acme"; then
    pass "the valid key is written to .env.acme"
else
    fail ".env.acme was not written"
fi

# 8. A missing right is named and nothing is written.
write_acme_env
rm -f "$tmp/.env.acme"
if OVH_CASE=missing run_acme --acme-zone example.com; then
    fail "the installer accepted a key with a missing right"
else
    pass "a missing right stops the installer"
fi
if grep -qF 'missing right: DELETE /domain/zone/example.com/record/*' "$tmp/out"; then
    pass "the missing right is named"
else
    fail "the missing right is not named"
fi
if [ -e "$tmp/.env.acme" ]; then
    fail ".env.acme was written after a missing right"
else
    pass "nothing is written after a missing right"
fi

# 9. HTTP 403 is rejected with the message and without secrets.
write_acme_env
rm -f "$tmp/.env.acme"
if OVH_CASE=forbidden run_acme --acme-zone example.com; then
    fail "the installer accepted an HTTP 403"
else
    pass "an HTTP 403 stops the installer"
fi
if grep -q 'OVH rejected the key' "$tmp/err" && grep -q 'This service is not available' "$tmp/err"; then
    pass "the OVH rejection and its message are shown"
else
    fail "the OVH rejection message is missing"
fi
if [ -e "$tmp/.env.acme" ]; then
    fail ".env.acme was written after an HTTP 403"
else
    pass "nothing is written after an HTTP 403"
fi

# 10. A key pending validation is explained.
write_acme_env
rm -f "$tmp/.env.acme"
if OVH_CASE=pending run_acme --acme-zone example.com; then
    fail "the installer accepted a key pending validation"
else
    pass "a key pending validation stops the installer"
fi
if grep -q 'not validated' "$tmp/err"; then
    pass "the pending validation is explained"
else
    fail "the pending validation message is missing"
fi

# 11. An unreachable OVH is reported.
write_acme_env
rm -f "$tmp/.env.acme"
if OVH_CASE=unreachable run_acme --acme-zone example.com; then
    fail "the installer accepted an unreachable OVH"
else
    pass "an unreachable OVH stops the installer"
fi
if grep -q 'could not reach OVH' "$tmp/err"; then
    pass "the unreachable OVH is reported"
else
    fail "the unreachable OVH message is missing"
fi

# 12. An existing .env.acme is validated and kept.
write_acme_env
cat > "$tmp/.env.acme" <<EOF
OVH_ENDPOINT=ovh-eu
OVH_APPLICATION_KEY=$ovh_key
OVH_APPLICATION_SECRET=$ovh_secret
OVH_CONSUMER_KEY=$ovh_consumer
EOF
chmod 600 "$tmp/.env.acme"
existing_before="$(cat "$tmp/.env.acme")"
: > "$curl_log"
: > "$config_log"
if CURL_LOG="$curl_log" CURL_CONFIG_LOG="$config_log" run_acme; then
    pass "an existing .env.acme is checked"
else
    fail "an existing .env.acme did not pass the check"
fi
if grep -q 'keeping the existing .env.acme' "$tmp/out"; then
    pass "the existing .env.acme is kept"
else
    fail "the existing .env.acme is not kept"
fi
if [ "$(cat "$tmp/.env.acme")" = "$existing_before" ]; then
    pass "the existing .env.acme is not rewritten"
else
    fail "the existing .env.acme changed"
fi

# 13. --no-acme-check skips the network for an existing .env.acme.
write_acme_env
cat > "$tmp/.env.acme" <<'EOF'
OVH_ENDPOINT=ovh-eu
OVH_APPLICATION_KEY=badkey
OVH_APPLICATION_SECRET=badsecret
OVH_CONSUMER_KEY=badconsumer
EOF
: > "$curl_log"
if CURL_LOG="$curl_log" run_acme --no-acme-check; then
    pass "--no-acme-check keeps an existing .env.acme"
else
    fail "--no-acme-check did not skip the OVH check"
fi
if grep -q 'skipping the OVH check' "$tmp/out" && grep -q 'keeping the existing .env.acme' "$tmp/out"; then
    pass "the offline check is announced"
else
    fail "the offline check message is missing"
fi
if [ ! -s "$curl_log" ]; then
    pass "--no-acme-check makes no OVH call"
else
    fail "--no-acme-check called OVH"
fi

# 14. The Canadian link and a new .env.acme with --no-acme-check.
write_acme_env
rm -f "$tmp/.env.acme"
: > "$curl_log"
if ACME_ENDPOINT=ovh-ca CURL_LOG="$curl_log" run_acme --no-acme-check; then
    pass "--no-acme-check writes a new .env.acme"
else
    fail "--no-acme-check did not write .env.acme"
fi
if grep -q 'ca.api.ovh.com/createToken' "$tmp/out"; then
    pass "the Canadian key link is printed"
else
    fail "the Canadian key link is missing"
fi
if grep -q 'OVH_ENDPOINT=ovh-ca' "$tmp/.env.acme" 2>/dev/null; then
    pass "the Canadian region is stored"
else
    fail "the Canadian region is not stored"
fi
if [ ! -s "$curl_log" ]; then
    pass "the new offline install makes no OVH call"
else
    fail "the new offline install called OVH"
fi

# --- Delegation checks ----------------------------------------------------

write_delegation_env() {
    cat > "$tmp/.env" <<'EOF'
POSTGRES_PASSWORD=test
TALAIA_PUBLIC_HOSTNAMES=talaia.example.com
TALAIA_PANEL_PORT=8443
TALAIA_AGENT_PORT=8444
TALAIA_ACME_PROVIDER=delegation
TALAIA_ACME_EMAIL=
EOF
}

run_delegation() {
    local status=0
    PATH="$stub:$system_path" \
        TALAIA_INSTALL_ACME_ONLY=1 \
        SYSTEM_PATH="$system_path" \
        STUB_DIR="$stub" \
        IP_ROUTE_OUTPUT="${IP_ROUTE_OUTPUT:-1.1.1.1 via 192.0.2.1 dev eth0 src 203.0.113.10 uid 0}" \
        SS_OUTPUT="${SS_OUTPUT:-UNCONN 0 0 127.0.0.53:53 0.0.0.0:*}" \
        DF_AVAILABLE=10000000 \
        "$bash_path" "$tmp/install.sh" "$@" < /dev/null >"$tmp/out" 2>"$tmp/err" || status=$?
    return "$status"
}

write_stub compose <<'EOF'
#!/bin/sh
if [ -n "${COMPOSE_LOG:-}" ]; then
    printf '%s\n' "$*" >> "$COMPOSE_LOG"
fi
case "${COMPOSE_MODE:-pass}" in
    pass)
        printf 'a-record: ok 203.0.113.10\naaaa-record: none\ndelegation: ok\ndns-server: ok\n'
        exit 0
        ;;
    failpass)
        if [ ! -f "${COMPOSE_COUNTER:?}" ]; then
            : > "$COMPOSE_COUNTER"
            printf 'dns-server: not delegated yet\n'
            exit 1
        fi
        printf 'a-record: ok\naaaa-record: none\ndelegation: ok\ndns-server: ok\n'
        exit 0
        ;;
    never)
        printf 'dns-server: not delegated yet\n'
        exit 1
        ;;
    staging)
        case "$*" in
            *"restart core"*) exit 0 ;;
            *"logs --since"*) printf '%s\n' '{"level":"INFO","msg":"ACME certificate ready"}'; exit 0 ;;
            *"up -d core"*) exit 0 ;;
            *) exit 0 ;;
        esac
        ;;
    stagingfail)
        case "$*" in
            *"restart core"*) exit 0 ;;
            *"logs --since"*) printf '%s\n' '{"level":"ERROR","msg":"ACME certificate renewal failed; keeping the current certificate: boom"}'; exit 0 ;;
            *"up -d core"*) exit 0 ;;
            *) exit 0 ;;
        esac
        ;;
esac
EOF

# 15. Public IPv4 detection: default route, fallback, private NAT and flag.
public_ip="$( export PATH="$stub:$system_path"; export IP_ROUTE_OUTPUT="1.1.1.1 via 192.0.2.1 dev eth0 src 203.0.113.10 uid 0"; public_ip_arg=""; delegation_public_ip=""; detect_public_ipv4; printf '%s' "$delegation_public_ip" )"
if [ "$public_ip" = "203.0.113.10" ]; then
    pass "the route source is the public IPv4"
else
    fail "the route source was not used"
fi
public_ip="$( export PATH="$stub:$system_path"; export IP_ROUTE_OUTPUT=""; export HOSTNAME_OUT="2001:db8::1 192.0.2.50"; public_ip_arg=""; delegation_public_ip=""; detect_public_ipv4; printf '%s' "$delegation_public_ip" )"
if [ "$public_ip" = "192.0.2.50" ]; then
    pass "hostname -I is the fallback"
else
    fail "the hostname -I fallback failed"
fi
if ( export PATH="$stub:$system_path"; export IP_ROUTE_OUTPUT="1.1.1.1 via 192.0.2.1 dev eth0 src 10.0.0.5 uid 0"; public_ip_arg=""; detect_public_ipv4 ) < /dev/null >"$tmp/out" 2>"$tmp/err"; then
    fail "a private address was accepted"
else
    pass "a private address stops without a terminal"
fi
if grep -q 'behind NAT' "$tmp/err"; then
    pass "the NAT case is explained"
else
    fail "the NAT message is missing"
fi
public_ip="$( export PATH="$stub:$system_path"; IP_ROUTE_OUTPUT="1.1.1.1 via 192.0.2.1 dev eth0 src 10.0.0.5 uid 0"; export IP_ROUTE_OUTPUT; public_ip_arg="198.51.100.7"; delegation_public_ip=""; detect_public_ipv4; printf '%s' "$delegation_public_ip" )"
if [ "$public_ip" = "198.51.100.7" ]; then
    pass "--public-ip overrides the route"
else
    fail "--public-ip did not override the route"
fi

# 16. Port 53: 127.x is fine; the wildcard and the public IPv4 conflict.
if ( export PATH="$stub:$system_path"; export SS_OUTPUT="UNCONN 0 0 127.0.0.53:53 0.0.0.0:*"; delegation_public_ip="203.0.113.10"; check_port_53 ) < /dev/null; then
    pass "a listener on 127.0.0.53 is fine"
else
    fail "127.0.0.53 was treated as a conflict"
fi
if ( export PATH="$stub:$system_path"; export SS_OUTPUT="UNCONN 0 0 0.0.0.0:53 0.0.0.0:*"; delegation_public_ip="203.0.113.10"; check_port_53 ) < /dev/null >"$tmp/out" 2>"$tmp/err"; then
    fail "0.0.0.0:53 was accepted"
else
    pass "0.0.0.0:53 is a conflict"
fi
if grep -q 'port 53' "$tmp/err"; then
    pass "the port 53 conflict is explained"
else
    fail "the port 53 message is missing"
fi
if ( export PATH="$stub:$system_path"; export SS_OUTPUT="LISTEN 0 4096 203.0.113.10:53 0.0.0.0:*"; delegation_public_ip="203.0.113.10"; check_port_53 ) < /dev/null; then
    fail "a listener on the public IPv4 was accepted"
else
    pass "a listener on the public IPv4 is a conflict"
fi

# Our own acme-dns, left running by an earlier run, is not a conflict; another
# process holding the port still is.
if ( export PATH="$stub:$system_path"; export SS_OUTPUT="LISTEN 0 4096 203.0.113.10:53 0.0.0.0:*"; delegation_public_ip="203.0.113.10"; compose=(echo abc123); check_port_53 ) < /dev/null >"$tmp/out" 2>"$tmp/err"; then
    pass "a rerun keeps its own running acme-dns"
else
    fail "a rerun took its own acme-dns for a conflict"
fi
if ( export PATH="$stub:$system_path"; export SS_OUTPUT="LISTEN 0 4096 203.0.113.10:53 0.0.0.0:*"; delegation_public_ip="203.0.113.10"; compose=(true); check_port_53 ) < /dev/null >"$tmp/out" 2>"$tmp/err"; then
    fail "a listener was accepted although acme-dns is not running"
else
    pass "a listener is a conflict when acme-dns is not running"
fi

# 17. set_env_key replaces, appends, keeps the other lines and mode, and rejects
# values that would break the file.
if (
    cd "$tmp"
    mode_supported=0
    probe="$(mktemp)"
    chmod 600 "$probe"
    [ "$(stat -c %a "$probe" 2>/dev/null)" = "600" ] && mode_supported=1
    rm -f "$probe"
    printf 'POSTGRES_PASSWORD=keep\nTALAIA_ACME_PROVIDER=old\nKEEP=value\n' > .env
    chmod 644 .env
    set_env_key TALAIA_ACME_PROVIDER delegation
    set_env_key NEW_KEY new-value
    grep -q '^POSTGRES_PASSWORD=keep$' .env || exit 1
    grep -q '^KEEP=value$' .env || exit 1
    grep -q '^TALAIA_ACME_PROVIDER=delegation$' .env || exit 1
    grep -q '^NEW_KEY=new-value$' .env || exit 1
    if [ "$mode_supported" -eq 1 ]; then
        mode="$(stat -c %a .env 2>/dev/null || echo 600)"
        [ "$mode" = "600" ] || exit 1
    fi
    set_env_key BAD_KEY 'two words' >/dev/null 2>&1 && exit 1
    set_env_key BAD_KEY 'a|b' >/dev/null 2>&1 && exit 1
    grep -q '^BAD_KEY=' .env && exit 1
    exit 0
); then
    pass "set_env_key edits the .env safely"
else
    fail "set_env_key failed"
fi

# 18. The zone guess and the two records.
zone="$( { export TALAIA_PUBLIC_HOSTNAMES="talaia.example.com"; acme_zone_arg=""; acme_zone_value=""; determine_acme_zone; printf '%s' "$acme_zone_value"; } < /dev/null )"
if [ "$zone" = "example.com" ]; then
    pass "the zone is guessed from the host name"
else
    fail "the zone guess failed"
fi
records="$( export delegation_host="talaia.example.com" delegation_zone="example.com" delegation_public_ip="203.0.113.10"; delegation_records )"
if grep -q '^A.*talaia.*203.0.113.10' <<< "$records" && grep -q '^NS.*_acme-challenge.talaia.*talaia.example.com\.' <<< "$records"; then
    pass "the delegation records use relative names"
else
    fail "the delegation records are wrong"
fi
if grep -q 'AAAA' <<< "$records" && grep -q 'IPv4 only' <<< "$records"; then
    pass "the AAAA warning is printed"
else
    fail "the AAAA warning is missing"
fi
apex="$( export delegation_host="example.com" delegation_zone="example.com" delegation_public_ip="203.0.113.10"; delegation_records )"
if grep -q '^A.*@.*203.0.113.10' <<< "$apex" && grep -q '^NS.*_acme-challenge ' <<< "$apex"; then
    pass "the apex records are printed"
else
    fail "the apex records are wrong"
fi

# 19. The check loop: pass, fail then pass, and never (ACME off, staging gone).
if (
    export PATH="$stub:$system_path" COMPOSE_MODE=pass
    cd "$tmp"
    compose=("$stub/compose")
    delegation_host="talaia.example.com"
    delegation_public_ip="203.0.113.10"
    delegation_check
) < /dev/null >"$tmp/out" 2>"$tmp/err"; then
    pass "the delegation check passes"
else
    fail "a passing delegation check failed"
fi
if grep -q 'dns-server: ok' "$tmp/out"; then
    pass "the check lines are shown"
else
    fail "the check lines are missing"
fi
rm -f "$tmp/check.counter"
if (
    export PATH="$stub:$system_path" COMPOSE_MODE=failpass COMPOSE_COUNTER="$tmp/check.counter"
    cd "$tmp"
    compose=("$stub/compose")
    delegation_host="talaia.example.com"
    delegation_public_ip="203.0.113.10"
    delegation_check
) < /dev/null >"$tmp/out" 2>"$tmp/err"; then
    pass "the check loop retries and passes"
else
    fail "the check loop did not pass after a retry"
fi
write_delegation_env
printf 'COMPOSE_PROFILES=acme-dns\nTALAIA_DNS_ADDRESS=203.0.113.10\nTALAIA_ACME_DIRECTORY_URL=https://staging.example/directory\n' >> "$tmp/.env"
if (
    export PATH="$stub:$system_path" COMPOSE_MODE=never
    cd "$tmp"
    compose=("$stub/compose")
    delegation_host="talaia.example.com"
    delegation_public_ip="203.0.113.10"
    delegation_check
) < /dev/null >"$tmp/out" 2>"$tmp/err"; then
    fail "the check loop passed when it never should"
else
    pass "the check loop gives up after ten attempts"
fi
if grep -q '^TALAIA_ACME_PROVIDER=$' "$tmp/.env" && ! grep -q '^TALAIA_ACME_DIRECTORY_URL=' "$tmp/.env"; then
    pass "giving up turns ACME off and removes staging"
else
    fail "the cleanup after the check loop is wrong"
fi

# 20. Staging to production with the stubbed logs.
write_delegation_env
printf 'COMPOSE_PROFILES=acme-dns\nTALAIA_DNS_ADDRESS=203.0.113.10\nTALAIA_ACME_DIRECTORY_URL=https://staging.example/directory\n' >> "$tmp/.env"
: > "$tmp/compose.log"
if (
    export PATH="$stub:$system_path" COMPOSE_MODE=staging COMPOSE_LOG="$tmp/compose.log"
    cd "$tmp"
    compose=("$stub/compose")
    delegation_watched=0
    delegation_staging
) < /dev/null >"$tmp/out" 2>"$tmp/err"; then
    pass "the staging certificate switches to production"
else
    fail "the staging switch failed"
fi
if grep -q 'staging certificate: ok (the delegation works from the internet)' "$tmp/out"; then
    pass "the staging success is reported"
else
    fail "the staging success is missing"
fi
if grep -q '^TALAIA_ACME_DIRECTORY_URL=$' "$tmp/.env"; then
    pass "production leaves an empty directory URL"
else
    fail "the production directory URL is not empty"
fi
if grep -q 'restart core' "$tmp/compose.log" && grep -q 'up -d core' "$tmp/compose.log"; then
    pass "the core is restarted and recreated"
else
    fail "the staging switch did not restart/recreate the core"
fi

# 21. The delegation setup writes the keys and the records (no build).
write_delegation_env
if run_delegation --acme delegation; then
    pass "the delegation setup completes"
else
    fail "the delegation setup failed"
fi
if grep -q '^COMPOSE_PROFILES=acme-dns$' "$tmp/.env" &&
    grep -q '^TALAIA_DNS_ADDRESS=203.0.113.10$' "$tmp/.env" &&
    grep -q '^TALAIA_ACME_DIRECTORY_URL=https://acme-staging-v02.api.letsencrypt.org/directory$' "$tmp/.env" &&
    grep -q '^TALAIA_ACME_PROVIDER=delegation$' "$tmp/.env"; then
    pass "the delegation keys are written"
else
    fail "the delegation keys are missing"
fi
if grep -q 'Create these two records' "$tmp/out" && grep -q '^A.*talaia.*203.0.113.10' "$tmp/out" && grep -q '^NS.*_acme-challenge.talaia.*talaia.example.com\.' "$tmp/out"; then
    pass "the setup prints the records"
else
    fail "the setup did not print the records"
fi
if grep -q 'acme: checked' "$tmp/out"; then
    pass "the setup stops before the build"
else
    fail "the setup did not stop before the build"
fi

# 22. The port 53 conflict and the NAT case stop, and --public-ip overrides.
write_delegation_env
if SS_OUTPUT="UNCONN 0 0 0.0.0.0:53 0.0.0.0:*" run_delegation --acme delegation; then
    fail "the setup accepted a port 53 conflict"
else
    pass "the setup stops on a port 53 conflict"
fi
if grep -q 'port 53' "$tmp/err"; then
    pass "the setup explains the port 53 conflict"
else
    fail "the setup did not explain the port 53 conflict"
fi
write_delegation_env
if IP_ROUTE_OUTPUT="1.1.1.1 via 192.0.2.1 dev eth0 src 10.0.0.5 uid 0" run_delegation --acme delegation; then
    fail "the setup accepted a private address"
else
    pass "the setup stops on a private address"
fi
if grep -q 'behind NAT' "$tmp/err"; then
    pass "the setup explains NAT"
else
    fail "the setup did not explain NAT"
fi
write_delegation_env
if IP_ROUTE_OUTPUT="1.1.1.1 via 192.0.2.1 dev eth0 src 10.0.0.5 uid 0" run_delegation --acme delegation --public-ip 198.51.100.7; then
    pass "--public-ip carries the setup through NAT"
else
    fail "--public-ip did not carry the setup"
fi
if grep -q '^TALAIA_DNS_ADDRESS=198.51.100.7$' "$tmp/.env"; then
    pass "--public-ip is written to .env"
else
    fail "--public-ip was not written"
fi

# 23. --no-acme explicitly turns ACME off and removes staging.
write_delegation_env
printf 'COMPOSE_PROFILES=acme-dns\nTALAIA_DNS_ADDRESS=203.0.113.10\nTALAIA_ACME_DIRECTORY_URL=https://staging.example/directory\n' >> "$tmp/.env"
if run_delegation --no-acme; then
    pass "--no-acme completes"
else
    fail "--no-acme failed"
fi
if grep -q '^TALAIA_ACME_PROVIDER=$' "$tmp/.env" && ! grep -q '^TALAIA_ACME_DIRECTORY_URL=' "$tmp/.env"; then
    pass "--no-acme turns ACME off and removes staging"
else
    fail "--no-acme left ACME or staging on"
fi
if run_delegation --acme delegation --no-acme; then
    fail "--acme and --no-acme were accepted together"
else
    pass "--acme and --no-acme conflict"
fi

# 24. A fresh .env with --acme delegation writes the keys and records.
rm -f "$tmp/.env"
if run_delegation --hostname talaia.example.com --acme delegation; then
    pass "a fresh delegation install completes the setup"
else
    fail "a fresh delegation install failed"
fi
if grep -q '^TALAIA_ACME_PROVIDER=delegation$' "$tmp/.env" &&
    grep -q '^COMPOSE_PROFILES=acme-dns$' "$tmp/.env" &&
    grep -q '^TALAIA_DNS_ADDRESS=203.0.113.10$' "$tmp/.env"; then
    pass "a fresh .env gets the delegation keys"
else
    fail "a fresh .env did not get the delegation keys"
fi

# 25. The bootstrap clones, reuses, refuses a foreign directory and passes
# the installer flags through. It needs real git (the test stubs it elsewhere).
if command -v git >/dev/null 2>&1; then
    bootstrap_repo="$tmp/remote.git"
    bootstrap_work="$tmp/work"
    git init -q --bare "$bootstrap_repo"
    git init -q "$bootstrap_work"
    git -C "$bootstrap_work" config user.email test@example.com
    git -C "$bootstrap_work" config user.name Test
    git -C "$bootstrap_work" config core.autocrlf false
    git -C "$bootstrap_work" checkout -q -b main
    mkdir -p "$bootstrap_work/packaging/core"
    cat > "$bootstrap_work/packaging/core/install.sh" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" > "${BOOTSTRAP_ARGS_LOG:?}"
exit 0
EOF
    chmod +x "$bootstrap_work/packaging/core/install.sh"
    git -C "$bootstrap_work" add .
    git -C "$bootstrap_work" commit -qm init
    git -C "$bootstrap_work" push -q "$bootstrap_repo" main

    run_bootstrap() {
        local target="$1"
        local status=0
        shift
        TALAIA_REPO_URL="${BOOTSTRAP_URL:-$bootstrap_repo}" TALAIA_BRANCH=main TALAIA_DIR="$target" \
            "$bash_path" "$bootstrap" "$@" < /dev/null >"$tmp/bout" 2>&1 || status=$?
        return "$status"
    }

    rm -rf "$tmp/clone"
    if TALAIA_BOOTSTRAP_NO_EXEC=1 run_bootstrap "$tmp/clone"; then
        pass "the bootstrap clones the repository"
    else
        fail "the bootstrap did not clone"
    fi
    if [ -d "$tmp/clone/.git" ]; then
        pass "the clone is a git repository"
    else
        fail "the clone is not a git repository"
    fi

    clone_url="$(git -C "$tmp/clone" remote get-url origin)"
    if TALAIA_BOOTSTRAP_NO_EXEC=1 BOOTSTRAP_URL="$clone_url" run_bootstrap "$tmp/clone"; then
        pass "the bootstrap reuses the clone"
    else
        fail "the bootstrap did not reuse the clone"
    fi
    if grep -q 'reusing the existing clone' "$tmp/bout"; then
        pass "the reuse is announced"
    else
        fail "the reuse is not announced"
    fi

    mkdir -p "$tmp/foreign"
    printf 'x\n' > "$tmp/foreign/file"
    if TALAIA_BOOTSTRAP_NO_EXEC=1 run_bootstrap "$tmp/foreign"; then
        fail "the bootstrap accepted a foreign directory"
    else
        pass "the bootstrap refuses a foreign directory"
    fi
    if grep -q 'not a Talaia clone' "$tmp/bout"; then
        pass "the foreign directory is explained"
    else
        fail "the foreign directory message is missing"
    fi

    rm -rf "$tmp/clone2"
    : > "$tmp/args.log"
    if BOOTSTRAP_ARGS_LOG="$tmp/args.log" TALAIA_INSTALL_PREFLIGHT_ONLY=1 \
        run_bootstrap "$tmp/clone2" --hostname talaia.example.com --acme ovh; then
        pass "the bootstrap runs the installer"
    else
        fail "the bootstrap did not run the installer"
    fi
    if grep -q -- '--hostname talaia.example.com --acme ovh' "$tmp/args.log"; then
        pass "the installer flags pass through the bootstrap"
    else
        fail "the installer flags did not pass through"
    fi

    rm -rf "$tmp/clone3"
    : > "$tmp/args.log"
    if cat "$bootstrap" |
        BOOTSTRAP_ARGS_LOG="$tmp/args.log" TALAIA_INSTALL_PREFLIGHT_ONLY=1 \
            TALAIA_REPO_URL="$bootstrap_repo" TALAIA_BRANCH=main \
            TALAIA_DIR="$tmp/clone3" "$bash_path" -s -- --hostname talaia.example.com --acme ovh \
            >"$tmp/bout" 2>&1; then
        pass "the piped bootstrap runs the installer"
    else
        fail "the piped bootstrap did not run"
    fi
    if grep -q -- '--hostname talaia.example.com --acme ovh' "$tmp/args.log"; then
        pass "the flags pass through bash -s --"
    else
        fail "the flags did not pass through bash -s --"
    fi
else
    echo "skipped: the bootstrap clone tests need git"
fi

if [ "$failures" -ne 0 ]; then
    echo "installer tests: $failures test(s) failed" >&2
    exit 1
fi
echo "installer tests: all tests passed"
