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

# The bootstrap has a small catalog of its own: it runs before the repository
# exists, so it cannot use the installer catalog. --lang wins, then
# LC_ALL/LC_MESSAGES/LANG, then English.
detect_lang() {
    local value="${LC_ALL:-${LC_MESSAGES:-${LANG:-}}}"
    case "$value" in
        ca*) printf 'ca\n' ;;
        es*) printf 'es\n' ;;
        *) printf 'en\n' ;;
    esac
}
lang="$(detect_lang)"

is_yes() {
    case "$1" in
        yes | Yes | YES | y | Y | si | Si | SI | sí | Sí | s | S) return 0 ;;
        *) return 1 ;;
    esac
}

msg() {
    local key="$1"
    shift
    case "$lang" in
        es) msg_es "$key" "$@" && return 0 ;;
        ca) msg_ca "$key" "$@" && return 0 ;;
    esac
    msg_en "$key" "$@"
}

msg_en() {
    local key="$1"
    shift
    case "$key" in
        root_needed) printf "error: creating %s needs root. Install sudo or set TALAIA_DIR to a directory you can write.\n" "$1" >&2 ;;
        git_os) printf "error: git is not installed. Install it with the package manager of this system and run again.\n" >&2 ;;
        git_cmd) printf "error: git is not installed. Install it with '%s' and run again.\n" "$1" >&2 ;;
        git_question) printf "Install git with apt-get? (yes/no) " ;;
        git_required) printf "error: git is required. Install it with '%s' and run again.\n" "$1" >&2 ;;
        git_installing) printf "installing git with apt-get\n" ;;
        git_still) printf "error: git is still not available; open a new shell and run this again.\n" >&2 ;;
        bash_needed) printf "error: this bootstrap must run with bash, not sh.\n" >&2 ;;
        intro) printf "Talaia: cloning %s (%s) into %s and running packaging/core/install.sh.\n" "$1" "$2" "$3" ;;
        sudo_note) printf "sudo is used only where it is needed; the installer asks before installing anything.\n" ;;
        target_file) printf "error: %s exists and is not a directory; move it away or set TALAIA_DIR.\n" "$1" >&2 ;;
        reuse) printf "reusing the existing clone in %s (updates come from the panel).\n" "$1" ;;
        other_repo) printf "error: %s is a clone of another repository; move it away or set TALAIA_DIR.\n" "$1" >&2 ;;
        not_clone) printf "error: %s exists and is not a Talaia clone; move it away or set TALAIA_DIR.\n" "$1" >&2 ;;
        cloning) printf "cloning %s (%s) into %s\n" "$1" "$2" "$3" ;;
        clone_failed) printf "error: the clone failed; check the address and the network and run again.\n" >&2 ;;
        clone_ready) printf "clone ready in %s\n" "$1" ;;
        no_exec) printf "bootstrap: clone ready in %s\n" "$1" ;;
        *) printf '%s\n' "$key" ;;
    esac
}

msg_es() {
    local key="$1"
    shift
    case "$key" in
        root_needed) printf "error: crear %s necesita root. Instala sudo o define TALAIA_DIR en un directorio donde puedas escribir.\n" "$1" >&2 ;;
        git_os) printf "error: git no está instalado. Instálalo con el gestor de paquetes de este sistema y vuelve a ejecutar.\n" >&2 ;;
        git_cmd) printf "error: git no está instalado. Instálalo con '%s' y vuelve a ejecutar.\n" "$1" >&2 ;;
        git_question) printf "¿Instalar git con apt-get? (sí/no) " ;;
        git_required) printf "error: git es necesario. Instálalo con '%s' y vuelve a ejecutar.\n" "$1" >&2 ;;
        git_installing) printf "instalando git con apt-get\n" ;;
        git_still) printf "error: git sigue sin estar disponible; abre una shell nueva y vuelve a ejecutar.\n" >&2 ;;
        bash_needed) printf "error: este arranque debe ejecutarse con bash, no con sh.\n" >&2 ;;
        intro) printf "Talaia: se clona %s (%s) en %s y se ejecuta packaging/core/install.sh.\n" "$1" "$2" "$3" ;;
        sudo_note) printf "sudo solo se usa donde hace falta; el instalador pregunta antes de instalar nada.\n" ;;
        target_file) printf "error: %s existe y no es un directorio; muévelo o define TALAIA_DIR.\n" "$1" >&2 ;;
        reuse) printf "se reutiliza el clon existente en %s (las actualizaciones vienen del panel).\n" "$1" ;;
        other_repo) printf "error: %s es un clon de otro repositorio; muévelo o define TALAIA_DIR.\n" "$1" >&2 ;;
        not_clone) printf "error: %s existe y no es un clon de Talaia; muévelo o define TALAIA_DIR.\n" "$1" >&2 ;;
        cloning) printf "clonando %s (%s) en %s\n" "$1" "$2" "$3" ;;
        clone_failed) printf "error: el clon ha fallado; revisa la dirección y la red y vuelve a ejecutar.\n" >&2 ;;
        clone_ready) printf "clon listo en %s\n" "$1" ;;
        no_exec) printf "bootstrap: clon listo en %s\n" "$1" ;;
        *) return 1 ;;
    esac
}

msg_ca() {
    local key="$1"
    shift
    case "$key" in
        root_needed) printf "error: crear %s necessita root. Instal·la sudo o defineix TALAIA_DIR en un directori on puguis escriure.\n" "$1" >&2 ;;
        git_os) printf "error: git no està instal·lat. Instal·la'l amb el gestor de paquets d'aquest sistema i torna a executar.\n" >&2 ;;
        git_cmd) printf "error: git no està instal·lat. Instal·la'l amb '%s' i torna a executar.\n" "$1" >&2 ;;
        git_question) printf "Vols instal·lar git amb apt-get? (sí/no) " ;;
        git_required) printf "error: cal git. Instal·la'l amb '%s' i torna a executar.\n" "$1" >&2 ;;
        git_installing) printf "instal·lant git amb apt-get\n" ;;
        git_still) printf "error: git encara no està disponible; obre una shell nova i torna a executar.\n" >&2 ;;
        bash_needed) printf "error: aquest arrencament s'ha d'executar amb bash, no amb sh.\n" >&2 ;;
        intro) printf "Talaia: es clona %s (%s) a %s i s'executa packaging/core/install.sh.\n" "$1" "$2" "$3" ;;
        sudo_note) printf "sudo només s'usa on cal; l'instal·lador pregunta abans d'instal·lar res.\n" ;;
        target_file) printf "error: %s existeix i no és un directori; mou-lo o defineix TALAIA_DIR.\n" "$1" >&2 ;;
        reuse) printf "es reutilitza el clon existent a %s (les actualitzacions vénen del panell).\n" "$1" ;;
        other_repo) printf "error: %s és un clon d'un altre repositori; mou-lo o defineix TALAIA_DIR.\n" "$1" >&2 ;;
        not_clone) printf "error: %s existeix i no és un clon de Talaia; mou-lo o defineix TALAIA_DIR.\n" "$1" >&2 ;;
        cloning) printf "clonant %s (%s) a %s\n" "$1" "$2" "$3" ;;
        clone_failed) printf "error: el clon ha fallat; revisa l'adreça i la xarxa i torna a executar.\n" >&2 ;;
        clone_ready) printf "clon a punt a %s\n" "$1" ;;
        no_exec) printf "bootstrap: clon a punt a %s\n" "$1" ;;
        *) return 1 ;;
    esac
}

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
        msg root_needed "${TALAIA_DIR:-/opt/talaia}"
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
        msg git_os
        exit 1
    fi
    if [ ! -t 0 ]; then
        msg git_cmd "sudo apt-get install -y git"
        exit 1
    fi

    local answer=""
    read -r -p "$(msg git_question)" answer || true
    if ! is_yes "$answer"; then
        msg git_required "sudo apt-get install -y git"
        exit 1
    fi
    msg git_installing
    if [ "$(id -u)" -eq 0 ]; then
        apt-get update
        apt-get install -y git
    else
        sudo apt-get update
        sudo apt-get install -y git
    fi
    if ! command -v git >/dev/null 2>&1; then
        msg git_still
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
        msg bash_needed
        exit 1
    fi

    local repo_url="${TALAIA_REPO_URL:-https://github.com/catlinux/Talaia.git}"
    local branch="${TALAIA_BRANCH:-main}"
    local target="${TALAIA_DIR:-/opt/talaia}"
    local bash_bin owner group prev=""
    local arg

    for arg in "$@"; do
        if [ "$prev" = "--lang" ]; then
            case "$arg" in
                en | es | ca) lang="$arg" ;;
            esac
        fi
        prev="$arg"
    done

    msg intro "$repo_url" "$branch" "$target"
    msg sudo_note
    echo

    bash_bin="$(command -v bash 2>/dev/null || true)"
    [ -n "$bash_bin" ] || bash_bin=/bin/bash

    ensure_git

    if [ -e "$target" ] && [ ! -d "$target" ]; then
        msg target_file "$target"
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
            msg reuse "$target"
        else
            msg other_repo "$target"
            exit 1
        fi
    elif [ -n "$(ls -A "$target" 2>/dev/null || true)" ]; then
        msg not_clone "$target"
        exit 1
    else
        msg cloning "$repo_url" "$branch" "$target"
        if ! git clone --branch "$branch" "$repo_url" "$target"; then
            msg clone_failed
            exit 1
        fi
        if [ "$(id -u)" -eq 0 ] && [ -n "${SUDO_USER:-}" ] && [ "$SUDO_USER" != "root" ]; then
            owner="$(target_owner)"
            group="$(id -gn "$owner" 2>/dev/null || id -gn)"
            chown -R "$owner:$group" "$target"
        fi
        msg clone_ready "$target"
    fi

    # The tests stop after the clone; this is not an option for users.
    if [ "${TALAIA_BOOTSTRAP_NO_EXEC:-0}" = "1" ]; then
        msg no_exec "$target"
        exit 0
    fi

    exec "$bash_bin" "$target/packaging/core/install.sh" "$@"
}

main "$@"
