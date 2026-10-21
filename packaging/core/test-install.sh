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
echo 192.0.2.10
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

# 15. The bootstrap clones, reuses, refuses a foreign directory and passes
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
