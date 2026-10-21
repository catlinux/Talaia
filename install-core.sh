#!/usr/bin/env bash
#
# One-line bootstrap for the Talaia core: clones the repository into
# /opt/talaia (TALAIA_DIR) and runs packaging/core/install.sh with the flags
# given. Download it and pipe it to bash:
#
#   curl -fsSL https://raw.githubusercontent.com/catlinux/Talaia/main/install-core.sh | bash
#   curl -fsSL https://raw.githubusercontent.com/catlinux/Talaia/main/install-core.sh | bash -s -- --hostname talaia.example.com --acme ovh
#
# Environment:
#   TALAIA_DIR       where to clone it (default: /opt/talaia)
#   TALAIA_REPO_URL  repository to clone (default: https://github.com/catlinux/Talaia.git)
#   TALAIA_BRANCH    branch to clone (default: main)
#
# It needs bash and git; if git is missing on Debian or Ubuntu it can install it
# with apt-get after asking. sudo is used only to create the directory when its
# parent is not writable and to install git; the installer asks before it
# installs anything else.
set -eu

# target_owner prints the user that must own the clone: the invoking user when
# the script runs through sudo, so the update helper can run git as that user.
target_owner() {
    if [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != "root" ]; then
        printf '%s\n' "$SUDO_USER"
    else
        id -un
    fi
}

# run_root runs a command as root, with sudo when the script is not root.
run_root() {
    if [ "$(id -u)" -eq 0 ]; then
        "$@"
        return 0
    fi
    if ! command -v sudo >/dev/null 2>&1; then
        echo "error: creating ${TALAIA_DIR:-/opt/talaia} needs root. Install sudo or set TALAIA_DIR to a directory you can write." >&2
        exit 1
    fi
    sudo "$@"
}

# ensure_git stops when git is missing, offering the apt-get install on Debian
# and Ubuntu when it can ask.
ensure_git() {
    if command -v git >/dev/null 2>&1; then
        return 0
    fi

    local os_id=""
    if [ -r /etc/os-release ]; then
        os_id="$(sed -n 's/^ID=//p' /etc/os-release 2>/dev/null | tr -d '"' | head -n 1)"
    fi
    if [ "$os_id" != "debian" ] && [ "$os_id" != "ubuntu" ]; then
        echo "error: git is not installed. Install it with the package manager of this system and run again." >&2
        exit 1
    fi
    if [ ! -t 0 ]; then
        echo "error: git is not installed. Install it with 'sudo apt-get install -y git' and run again." >&2
        exit 1
    fi

    local answer=""
    read -r -p "Install git with apt-get? (yes/no) " answer || true
    if [ "$answer" != "yes" ]; then
        echo "error: git is required. Install it with 'sudo apt-get install -y git' and run again." >&2
        exit 1
    fi
    echo "installing git with apt-get"
    if [ "$(id -u)" -eq 0 ]; then
        apt-get update
        apt-get install -y git
    else
        sudo apt-get update
        sudo apt-get install -y git
    fi
    if ! command -v git >/dev/null 2>&1; then
        echo "error: git is still not available; open a new shell and run this again." >&2
        exit 1
    fi
}

# normalize_remote drops a trailing slash and .git so the same repository given
# in two forms compares equal.
normalize_remote() {
    local url="${1%/}"
    printf '%s\n' "${url%.git}"
}

# same_remote reports whether the clone in $1 has the repository in $2 as origin.
same_remote() {
    local have
    have="$(git -C "$1" remote get-url origin 2>/dev/null || true)"
    [ -n "$have" ] || return 1
    [ "$(normalize_remote "$have")" = "$(normalize_remote "$2")" ]
}

main() {
    # Through a pipe stdin is not a terminal: reconnect it so the installer and
    # the questions below can ask on the terminal that started the pipe. When
    # there is no usable /dev/tty the installer runs non-interactively and asks
    # for --hostname.
    if [ ! -t 0 ] && [ -r /dev/tty ]; then
        { exec < /dev/tty; } 2>/dev/null || true
    fi

    if [ -z "${BASH_VERSION:-}" ]; then
        echo "error: this bootstrap must run with bash, not sh." >&2
        exit 1
    fi

    local repo_url="${TALAIA_REPO_URL:-https://github.com/catlinux/Talaia.git}"
    local branch="${TALAIA_BRANCH:-main}"
    local target="${TALAIA_DIR:-/opt/talaia}"
    local bash_bin owner group

    echo "Talaia: cloning $repo_url ($branch) into $target and running packaging/core/install.sh."
    echo "sudo is used only where it is needed; the installer asks before installing anything."
    echo

    bash_bin="$(command -v bash 2>/dev/null || true)"
    [ -n "$bash_bin" ] || bash_bin=/bin/bash

    ensure_git

    if [ -e "$target" ] && [ ! -d "$target" ]; then
        echo "error: $target exists and is not a directory; move it away or set TALAIA_DIR." >&2
        exit 1
    fi
    if [ ! -d "$target" ]; then
        if ! mkdir -p "$target" 2>/dev/null; then
            run_root mkdir -p "$target"
            owner="$(target_owner)"
            group="$(id -gn "$owner" 2>/dev/null || id -gn)"
            run_root chown "$owner:$group" "$target"
        elif [ "$(id -u)" -eq 0 ] && [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != "root" ]; then
            owner="$(target_owner)"
            group="$(id -gn "$owner" 2>/dev/null || id -gn)"
            chown "$owner:$group" "$target"
        fi
    fi

    if [ -d "$target/.git" ]; then
        if same_remote "$target" "$repo_url"; then
            echo "reusing the existing clone in $target (updates come from the panel)."
        else
            echo "error: $target is a clone of another repository; move it away or set TALAIA_DIR." >&2
            exit 1
        fi
    elif [ -n "$(ls -A "$target" 2>/dev/null || true)" ]; then
        echo "error: $target exists and is not a Talaia clone; move it away or set TALAIA_DIR." >&2
        exit 1
    else
        echo "cloning $repo_url ($branch) into $target"
        if ! git clone --branch "$branch" "$repo_url" "$target"; then
            echo "error: the clone failed; check the address and the network and run again." >&2
            exit 1
        fi
        if [ "$(id -u)" -eq 0 ] && [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != "root" ]; then
            owner="$(target_owner)"
            group="$(id -gn "$owner" 2>/dev/null || id -gn)"
            chown -R "$owner:$group" "$target"
        fi
        echo "clone ready in $target"
    fi

    # The tests stop after the clone; this is not an option for users.
    if [ "${TALAIA_BOOTSTRAP_NO_EXEC:-0}" = "1" ]; then
        echo "bootstrap: clone ready in $target"
        exit 0
    fi

    exec "$bash_bin" "$target/packaging/core/install.sh" "$@"
}

main "$@"
