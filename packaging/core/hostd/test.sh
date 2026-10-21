#!/usr/bin/env bash
#
# Tests talaia-hostd without root, Docker or a real core: git repositories are
# real, docker and curl are stubs that record what they were asked to do.
# Usage: packaging/core/hostd/test.sh
set -euo pipefail

HOSTD="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/talaia-hostd"
T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT

failures=0
pass() { echo "ok   - $1"; }
fail() {
    echo "FAIL - $1"
    failures=$((failures + 1))
}
check() {
    local name="$1"
    shift
    if "$@"; then pass "$name"; else fail "$name"; fi
}

git_q() { git -c user.name=test -c user.email=test@example.com -c init.defaultBranch=main "$@" > /dev/null 2>&1; }

# origin: main is A-B-C-D; X is on another branch.
git_q init --bare "$T/origin.git"
git_q clone "$T/origin.git" "$T/seed"
mkdir -p "$T/seed/packaging/core"
for name in A B C D; do
    echo "$name" > "$T/seed/packaging/core/version.txt"
    git_q -C "$T/seed" add -A
    git_q -C "$T/seed" commit -m "$name"
    eval "SHA_$name=\$(git -C \"\$T/seed\" rev-parse HEAD)"
done
git_q -C "$T/seed" push origin main
git_q -C "$T/seed" checkout -b other "$SHA_B"
echo X > "$T/seed/packaging/core/version.txt"
git_q -C "$T/seed" commit -am X
SHA_X="$(git -C "$T/seed" rev-parse HEAD)"
git_q -C "$T/seed" push origin other

git_q clone "$T/origin.git" "$T/repo"
git_q -C "$T/repo" reset --hard "$SHA_A"
printf 'TALAIA_PANEL_PORT=9443\n' > "$T/repo/packaging/core/.env"
printf '.env\n' > "$T/repo/.git/info/exclude"

mkdir -p "$T/host/requests" "$T/bin"
cat > "$T/hostd.conf" << EOF
# test configuration
REPO_DIR=$T/repo
REPO_USER=$(id -un)
HOST_DIR=$T/host
STATE_DIR=$T/state
HEALTH_TIMEOUT=1
EOF

# docker stub: the "running core" is the commit of the last successful up.
cat > "$T/bin/docker" << 'EOF'
#!/usr/bin/env bash
T="${STUB_DIR:?}"
echo "$*" >> "$T/docker.log"
case "$*" in
    "compose ps -q core") cat "$T/core_id" 2> /dev/null || true ;;
    "compose exec -T db pg_dump"*) echo "-- dump of $(cat "$T/running" 2> /dev/null)" ;;
    "compose exec -T db psql"*) cat > "$T/restored.sql" ;;
    "compose stop core") ;;
    "compose up -d --build")
        echo "up $TALAIA_GIT_COMMIT" >> "$T/docker.log"
        [ ! -e "$T/fail_build" ] || exit 1
        echo "$TALAIA_GIT_COMMIT" > "$T/running"
        echo "id-$TALAIA_GIT_COMMIT" > "$T/core_id"
        ;;
    *) echo "unexpected docker call: $*" >&2; exit 1 ;;
esac
EOF
# curl stub: /healthz answers unless the running commit is the bad one.
cat > "$T/bin/curl" << 'EOF'
#!/usr/bin/env bash
T="${STUB_DIR:?}"
case "$*" in *"https://127.0.0.1:9443/healthz"*) ;; *) exit 7 ;; esac
[ "$(cat "$T/running" 2> /dev/null)" != "$(cat "$T/bad_commit" 2> /dev/null)" ]
EOF
chmod +x "$T/bin/docker" "$T/bin/curl"
echo "$SHA_A" > "$T/running"
echo "id-$SHA_A" > "$T/core_id"

run_hostd() {
    STUB_DIR="$T" PATH="$T/bin:$PATH" TALAIA_HOSTD_CONF="$T/hostd.conf" bash "$HOSTD" update > "$T/out.log" 2>&1
}
request() {
    rm -f "$T/host/status.json"
    printf '{"target":"%s"}' "$1" > "$T/host/requests/update.json"
    run_hostd
}
state_is() { grep -q "\"state\":\"$1\"" "$T/host/status.json"; }
head_is() { [ "$(git -C "$T/repo" rev-parse HEAD)" = "$1" ]; }
log_has() { grep -q "$1" "$T/host/status.json"; }
no_request() { [ ! -e "$T/host/requests/update.json" ]; }
backups() { find "$T/state/backups" -name 'talaia-*.sql.gz' 2> /dev/null | wc -l; }

# 1. A valid request updates, stamps the commit and backs up first.
request "$SHA_B"
check "valid request succeeds" state_is succeeded
check "the clone moved to B" head_is "$SHA_B"
check "the image was built with GIT_COMMIT=B" grep -q "up $SHA_B" "$T/docker.log"
check "the request was consumed" no_request
check "one backup was taken" [ "$(backups)" -eq 1 ]
check "the backup is private" [ "$(stat -c %a "$(find "$T/state/backups" -name '*.sql.gz' | head -n 1)")" = 600 ]
check "the status is readable by the core" [ "$(stat -c %a "$T/host/status.json")" = 644 ]

# 2. A commit that is not on main is refused before anything changes.
: > "$T/docker.log"
request "$SHA_X"
check "other branch is refused" state_is failed
check "other branch reason" log_has "is not on origin/main"
check "other branch leaves the clone" head_is "$SHA_B"
check "other branch builds nothing" bash -c "! grep -q 'up ' '$T/docker.log'"

# 3. Malformed requests are refused.
for body in 'not json' '{"target":"main"}' "{\"target\":\"$SHA_C\",\"path\":\"/etc\"}" "{\"target\":\"../$SHA_C\"}"; do
    rm -f "$T/host/status.json"
    printf '%s' "$body" > "$T/host/requests/update.json"
    run_hostd
    check "malformed request refused: $body" state_is failed
    check "malformed request leaves the clone: $body" head_is "$SHA_B"
done

# 4. A link is not followed, even to a valid-looking request.
printf '{"target":"%s"}' "$SHA_C" > "$T/outside.json"
rm -f "$T/host/status.json"
ln -s "$T/outside.json" "$T/host/requests/update.json"
run_hostd
check "a symlinked request is refused" state_is failed
check "the link target is untouched" [ -f "$T/outside.json" ]
check "the symlinked request leaves the clone" head_is "$SHA_B"

# 4b. A directory instead of a file is refused and does not block the next request.
rm -f "$T/host/status.json"
mkdir "$T/host/requests/update.json"
run_hostd
check "a directory request is refused" state_is failed
check "the directory request is consumed" no_request

# 5. Going back in history is refused.
request "$SHA_A"
check "an older commit is refused" log_has "is not newer than"
check "an older commit leaves the clone" head_is "$SHA_B"

# 6. A build that fails before replacing the core only resets the clone.
touch "$T/fail_build"
rm -f "$T/restored.sql"
request "$SHA_C"
rm -f "$T/fail_build"
check "failed build rolls back" state_is rolled_back
check "failed build resets the clone" head_is "$SHA_B"
check "failed build keeps the database" [ ! -e "$T/restored.sql" ]

# 7. A core that does not answer is rolled back with the database backup.
echo "$SHA_D" > "$T/bad_commit"
request "$SHA_D"
check "unhealthy core rolls back" state_is rolled_back
check "unhealthy core resets the clone" head_is "$SHA_B"
check "the database backup was restored" grep -q "DROP SCHEMA IF EXISTS public CASCADE" "$T/restored.sql"
check "the previous core runs again" [ "$(cat "$T/running")" = "$SHA_B" ]
rm -f "$T/bad_commit"

# 8. Only the last three backups are kept.
request "$SHA_C"
request "$SHA_D"
check "update after rollbacks succeeds" state_is succeeded
check "no more than three backups" [ "$(backups)" -le 3 ]

# 9. An update cut short is reported at the next start.
printf '{"state":"running","from":"","to":"","started_at":"","finished_at":"","log":[]}\n' > "$T/host/status.json"
run_hostd
check "an interrupted update is reported" log_has "was interrupted"

# 10. Nothing to do: no request, no change.
cp "$T/host/status.json" "$T/status.before"
run_hostd
check "without a request nothing changes" cmp -s "$T/status.before" "$T/host/status.json"

# 11. Configuration with unknown keys is refused.
echo 'EVIL=1' >> "$T/hostd.conf"
if run_hostd; then fail "unknown configuration key refused"; else pass "unknown configuration key refused"; fi

if [ "$failures" -gt 0 ]; then
    echo "$failures check(s) failed; last helper output:"
    cat "$T/out.log"
    exit 1
fi
echo "all checks passed"
