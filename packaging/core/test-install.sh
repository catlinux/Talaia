#!/usr/bin/env bash
#
# Tests the preflight of install.sh with stub commands: no real Docker, no sudo
# and no files written outside a temporary directory. It runs on Debian, Ubuntu
# and Git Bash on Windows.
set -eu

script_path="${BASH_SOURCE[0]}"
case "$script_path" in
    */*) script_dir="${script_path%/*}" ;;
    *) script_dir="." ;;
esac
script_dir="$(cd "$script_dir" && pwd)"
installer="$script_dir/install.sh"
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
fi
exit 0
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
        "$bash_path" "$tmp/install.sh" "$@" >"$tmp/out" 2>"$tmp/err" || status=$?
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

if [ "$failures" -ne 0 ]; then
    echo "installer preflight: $failures test(s) failed" >&2
    exit 1
fi
echo "installer preflight: all tests passed"
