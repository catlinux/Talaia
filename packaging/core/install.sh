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

# Messages are selected by language: --lang wins, then LC_ALL/LC_MESSAGES/LANG,
# then English. The English strings are the original output, byte for byte, so
# scripts and tests that parse it keep working with LANG=C.
detect_lang() {
    local value="${LC_ALL:-${LC_MESSAGES:-${LANG:-}}}"
    case "$value" in
        ca*) printf 'ca\n' ;;
        es*) printf 'es\n' ;;
        *) printf 'en\n' ;;
    esac
}
lang="$(detect_lang)"

# is_yes keeps yes/no answers working and accepts the Spanish and Catalan forms.
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
        err_lang_value) printf "error: --lang needs a value\n" >&2 ;;
        err_lang) printf "error: unsupported language %s (supported: en, es, ca)\n" "$1" >&2 ;;
        err_hostname_value) printf "error: --hostname needs a value\n" >&2 ;;
        err_port_value) printf "error: --port needs a value\n" >&2 ;;
        err_agent_port_value) printf "error: --agent-port needs a value\n" >&2 ;;
        err_acme_value) printf "error: --acme needs a value\n" >&2 ;;
        err_acme_email_value) printf "error: --acme-email needs a value\n" >&2 ;;
        err_acme_zone_value) printf "error: --acme-zone needs a value\n" >&2 ;;
        err_public_ip_value) printf "error: --public-ip needs a value\n" >&2 ;;
        err_username_value) printf "error: --username needs a value\n" >&2 ;;
        unknown_option) printf "error: unknown option: %s\n" "$1" >&2 ;;
        ports_numbers) printf "error: --port and --agent-port must be numbers\n" >&2 ;;
        ports_differ) printf "error: --port and --agent-port must differ\n" >&2 ;;
        acme_unsupported) printf "error: unsupported ACME provider %s (supported: delegation, ovh)\n" "$1" >&2 ;;
        acme_no_conflict) printf "error: use either --acme or --no-acme, not both\n" >&2 ;;
        no_free_port) printf "error: no free port found from %s; pass --port and --agent-port\n" "$1" >&2 ;;
        dns_no_getent) printf "warning: cannot check the DNS record of %s (getent is not available)\n" "$1" >&2 ;;
        dns_mismatch) printf "warning: %s resolves to %s, which is not an address of this machine (%s). This is normal behind provider NAT; otherwise check the DNS record.\n" "$1" "$2" "$3" >&2 ;;
        dns_unresolved) printf "warning: %s does not resolve yet. Add a DNS A/AAAA record pointing to this server; browsers will not find the panel until it exists.\n" "$1" >&2 ;;
        fw_note_ufw) printf "note: ufw is installed; run 'sudo ufw status' to see whether it is active\n" >&2 ;;
        fw_note_firewalld) printf "note: firewalld is installed; run 'sudo firewall-cmd --state' to see whether it is running\n" >&2 ;;
        fw_ufw_active) printf "ufw is active. Open the two ports with:\n" ;;
        fw_firewalld_active) printf "firewalld is active. Open the two ports with:\n" ;;
        fw_none) printf "No active ufw or firewalld was detected.\n" ;;
        fw_provider) printf "Also open TCP %s and %s in the firewall or security group of your provider; the installer never opens ports.\n" "$1" "$2" ;;
        pf_os_missing) printf "warning: could not read /etc/os-release; this installer is tested on Debian and Ubuntu\n" >&2 ;;
        pf_os_other) printf "warning: %s is not Debian or Ubuntu; this installer is tested on those and the next steps may differ\n" "$1" >&2 ;;
        pf_git) printf "error: git is required to stamp the commit used by updates. Install it with '%s' (or the equivalent of your distribution) and run again.\n" "$1" >&2 ;;
        pf_curl) printf "error: curl is required to wait for the core and check it. Install it with '%s' (or the equivalent of your distribution) and run again.\n" "$1" >&2 ;;
        pf_root) printf "error: this installer needs root or sudo to install the update helper and Docker. Run it as root or install sudo.\n" >&2 ;;
        pf_sudo_note) printf "note: sudo will ask for your password when it is needed\n" ;;
        pf_disk_read) printf "warning: could not read the free disk space of %s\n" "$1" >&2 ;;
        pf_disk_low) printf "error: less than 2 GiB free on %s. Free space (for example: docker system prune -af, or remove old images and builds) and run again.\n" "$1" >&2 ;;
        pf_disk_warn) printf "warning: less than 5 GiB free on %s; the first build needs room for images and layers\n" "$1" >&2 ;;
        pf_docker_missing) printf "Docker is not installed. Talaia runs the core and its database as containers.\n" ;;
        pf_docker_official) printf "Official installation: %s\n" "$1" ;;
        pf_docker_installing) printf "installing Docker with the official script\n" ;;
        pf_docker_failed) printf "error: Docker installation failed. Install it with '%s' and run again.\n" "$1" >&2 ;;
        pf_docker_required) printf "error: Docker is required. Install it with '%s' (or pass --install-docker) and run again.\n" "$1" >&2 ;;
        pf_docker_still) printf "error: Docker is still not available. Open a new shell and run this installer again, or install it with '%s'.\n" "$1" >&2 ;;
        pf_docker_sudo) printf "note: this user cannot talk to Docker yet; the installer will use sudo. Log out and back in to stop using sudo.\n" ;;
        pf_compose) printf "error: the Docker Compose plugin is missing. Install it with '%s' (on some systems the package is '%s') and run again.\n" "$1" "$2" >&2 ;;
        pf_ok) printf "preflight: ok\n" ;;
        ask_docker) printf "Install Docker now with the official script? (yes/no)" ;;
        zone_missing) printf "error: pass --acme-zone <zone> (the registered domain) and run again.\n" >&2 ;;
        zone_invalid) printf "error: --acme-zone may only contain letters, digits, dots and hyphens\n" >&2 ;;
        zone_prompt) printf "DNS zone (the registered domain; for names like .co.uk type the registered domain)" ;;
        hostname_prompt) printf "Name or address the core is reached at" ;;
        email_prompt) printf "Contact address for the certificate authority (optional)" ;;
        cleanup_staging) printf "warning: the run ended before production; ACME is off and staging was removed.\n" >&2 ;;
        finish_later) printf "To finish later: %s\n" "$1" ;;
        wrote_acme) printf "wrote .env.acme\n" ;;
        keep_acme) printf "keeping the existing .env.acme\n" ;;
        ovh_sha1) printf "error: sha1sum is required to sign the OVH request (package coreutils).\n" >&2 ;;
        ovh_unreachable) printf "OVH: could not reach OVH at %s\n" "$1" >&2 ;;
        ovh_unreachable_http) printf "OVH: could not reach OVH at %s (HTTP %s)\n" "$1" "$2" >&2 ;;
        ovh_invalid_time) printf "OVH: OVH returned an invalid time\n" >&2 ;;
        ovh_key_ok) printf "OVH key: ok\n" ;;
        ovh_missing_right) printf "OVH key: missing right: %s\n" "$1" ;;
        ovh_rejected) printf "OVH rejected the key: %s\n" "$1" >&2 ;;
        ovh_not_validated) printf "OVH: the key was created but it is not validated yet. Open the validation page OVH shows (or the API keys page) and finish the validation, then run the installer again.\n" >&2 ;;
        ovh_rejected_http) printf "OVH rejected the key (HTTP %s): %s\n" "$1" "$2" >&2 ;;
        acme_file_incomplete) printf "error: .env.acme is incomplete; it needs OVH_APPLICATION_KEY, OVH_APPLICATION_SECRET and OVH_CONSUMER_KEY.\n" >&2 ;;
        acme_file_chars) printf "error: .env.acme contains unexpected characters; edit the three OVH values and run again.\n" >&2 ;;
        ovh_region_invalid) printf "error: the OVH region must be ovh-eu or ovh-ca (got %s)\n" "$1" >&2 ;;
        ovh_region_prompt) printf "OVH region (ovh-eu or ovh-ca)" ;;
        ovh_create_key) printf "Create an OVH API key for the zone %s with these three rights:\n" "$1" ;;
        ovh_guide) printf "Guide: %s (also docs/es/ovh-dns.md and docs/ca/ovh-dns.md).\n" "$1" ;;
        ovh_enter_values) printf "Press Enter when you have the three values: " ;;
        ovh_app_key) printf "OVH application key: " ;;
        ovh_secret) printf "OVH application secret: " ;;
        ovh_consumer) printf "OVH consumer key: " ;;
        ovh_missing_env) printf "error: set OVH_APPLICATION_KEY, OVH_APPLICATION_SECRET and OVH_CONSUMER_KEY (or run in a terminal)\n" >&2 ;;
        ovh_bad_chars) printf "error: the OVH key values contain unexpected characters; copy them again.\n" >&2 ;;
        ovh_skip) printf "skipping the OVH check (--no-acme-check)\n" ;;
        ovh_continue_unchecked) printf "warning: continuing without the OVH check\n" ;;
        ovh_validate_failed) printf "error: the OVH key could not be validated; fix it or pass --no-acme-check (offline installs).\n" >&2 ;;
        ovh_enter_again) printf "re-enter the three values\n" ;;
        ovh_off) printf "continuing with the Talaia CA; ACME is off.\n" ;;
        ovh_turned_off) printf "ACME turned off in packaging/core/.env.\n" ;;
        ovh_abort) printf "aborting without building anything.\n" >&2 ;;
        ovh_menu) printf "OVH check failed: [1] re-enter the keys, [2] continue without a trusted certificate (own CA, ACME off), [3] abort" ;;
        ovh_reach_prompt) printf "Could not reach OVH. Continue without validating the key? (yes/no)" ;;
        ip_not_found) printf "error: could not find the public IPv4 address; pass --public-ip <IPv4>.\n" >&2 ;;
        ip_invalid) printf "error: %s is not a valid IPv4 address; pass --public-ip <IPv4>.\n" "$1" >&2 ;;
        ip_nat) printf "This server is behind NAT: %s is a private address.\n" "$1" >&2 ;;
        ip_nat_fix) printf "error: pass the public IPv4 address with --public-ip <IPv4>.\n" >&2 ;;
        ip_prompt) printf "Public IPv4 address of this server" ;;
        ss_missing) printf "warning: ss is not available; cannot check whether port 53 is free.\n" >&2 ;;
        port53_own) printf "port 53 is held by the acme-dns container of this installation; keeping it.\n" ;;
        port53_conflict) printf "error: something is already listening on port 53 at %s%s.\n" "$1" "$2" >&2 ;;
        port53_need) printf "acme-dns needs UDP and TCP port 53 on the public IPv4 %s.\n" "$1" >&2 ;;
        env_key_invalid) printf "error: invalid .env key: %s\n" "$1" >&2 ;;
        env_value_invalid) printf "error: the value for %s contains a space, quote, newline or |.\n" "$1" >&2 ;;
        acme_off) printf "ACME is off; the panel uses the Talaia CA for now.\n" ;;
        rec_outside) printf "warning: %s is not inside %s; check the zone.\n" "$1" "$2" >&2 ;;
        rec_create) printf "Create these two records in the DNS zone of %s (at your DNS provider):\n" "$1" ;;
        rec_type) printf "Type" ;;
        rec_name) printf "Name" ;;
        rec_value) printf "Value" ;;
        rec_no_aaaa) printf "Do not create an AAAA record for %s: the challenge server answers on IPv4 only.\n" "$1" ;;
        rec_guide) printf "Guide: %s (also docs/es and docs/ca).\n" "$1" ;;
        dfw_note_ufw) printf "note: ufw is installed; run 'sudo ufw status' to see whether it is active.\n" >&2 ;;
        dfw_note_firewalld) printf "note: firewalld is installed; run 'sudo firewall-cmd --state' to see whether it is running.\n" >&2 ;;
        dfw_ports) printf "The delegation needs these ports open:\n" ;;
        dfw_ports_detail) printf "  TCP %s (panel), TCP %s (agents), UDP 53 and TCP 53 (acme-dns).\n" "$1" "$2" ;;
        dfw_ufw_question) printf "Open these ports in ufw now? (yes/no)" ;;
        dfw_firewalld_question) printf "Open these ports in firewalld now? (yes/no)" ;;
        dfw_opened_ufw) printf "opened the ports in ufw\n" ;;
        dfw_opened_firewalld) printf "opened the ports in firewalld\n" ;;
        dfw_ufw_active) printf "ufw is active. Open them with:\n" ;;
        dfw_firewalld_active) printf "firewalld is active. Open them with:\n" ;;
        dfw_installed_inactive) printf "ufw or firewalld is installed but not active.\n" ;;
        dfw_none) printf "No active ufw or firewalld was detected.\n" ;;
        dfw_provider) printf "Also allow TCP %s, TCP %s, UDP 53 and TCP 53 in the firewall or security group of your provider.\n" "$1" "$2" ;;
        dfw_never) printf "The installer never opens a port in the provider firewall.\n" ;;
        dfw_enter) printf "Press Enter when the records exist and the ports are open" ;;
        del_needs_dns) printf "error: the delegation needs a DNS name in --hostname (not an IP address).\n" >&2 ;;
        del_use_alt) printf "Use --acme ovh or --no-acme, or install with --hostname talaia.example.com.\n" >&2 ;;
        del_port53_in_use) printf "Port 53 is already in use, so acme-dns cannot bind the public IPv4.\n" ;;
        del_choice_ovh) printf "  1) use an OVH API key instead\n" ;;
        del_choice_off) printf "  2) continue without a trusted certificate for now (Talaia's own CA)\n" ;;
        del_choice_quit) printf "  3) quit\n" ;;
        del_choice) printf "Choice" ;;
        del_stop) printf "error: stop the process listening on port 53, or use --acme ovh or --no-acme.\n" >&2 ;;
        check_attempt) printf "checking the delegation with the public resolvers (attempt %s of %s)\n" "$1" "$2" ;;
        check_wait) printf "DNS changes can take a few minutes.\n" ;;
        check_menu) printf "Enter: check again, o: continue without a trusted certificate, q: quit" ;;
        check_unknown) printf "unknown choice; checking again\n" >&2 ;;
        check_gave_up) printf "error: the delegation check did not pass after %s attempts.\n" "$1" >&2 ;;
        staging_restart) printf "restarting the core to ask for the staging certificate\n" ;;
        staging_ok) printf "staging certificate: ok (the delegation works from the internet)\n" ;;
        staging_failed) printf "error: the staging certificate did not arrive within four minutes.\n" >&2 ;;
        staging_warning) printf "last ACME warning: %s\n" "$1" >&2 ;;
        staging_menu) printf "Staging failed: [1] retry, [2] continue without a trusted certificate, [3] quit" ;;
        hostd_no_systemd) printf "warning: no systemd here; updates from the panel stay off\n" >&2 ;;
        hostd_not_git) printf "warning: %s is not a git clone; updates from the panel stay off\n" "$1" >&2 ;;
        hostd_bad_path) printf "warning: the path %s has characters the helper refuses; updates from the panel stay off\n" "$1" >&2 ;;
        hostd_installing) printf "installing the update helper (talaia-hostd) for the clone in %s\n" "$1" ;;
        self_panel_ok) printf "panel: ok (%s)\n" "$1" ;;
        self_panel_failed) printf "panel: FAILED (%s); see: %s\n" "$1" "$2" ;;
        self_agents_ok) printf "agents: ok (%s)\n" "$1" ;;
        self_agents_failed) printf "agents: FAILED (%s); see: %s\n" "$1" "$2" ;;
        acme_obtained) printf "ACME certificate: obtained\n" ;;
        acme_pending_warning) printf "ACME certificate: still pending; last warning: %s\n" "$1" ;;
        acme_pending) printf "ACME certificate: still pending (the core keeps trying; see: %s)\n" "$1" ;;
        keep_env) printf "keeping the existing .env\n" ;;
        no_address) printf "error: could not find an address for this machine; pass --hostname\n" >&2 ;;
        port_busy) printf "error: port %s is already in use; pick another with --port\n" "$1" >&2 ;;
        agent_port_busy) printf "error: port %s is already in use; pick another with --agent-port\n" "$1" >&2 ;;
        cert_choice_title) printf "Which certificate should the panel use?\n" ;;
        cert_choice_1) printf "  1) automatic, with two DNS records (recommended)\n" ;;
        cert_choice_2) printf "  2) with an OVH API key\n" ;;
        cert_choice_3) printf "  3) none for now (Talaia's own CA)\n" ;;
        cert_choice) printf "Choice" ;;
        cert_choice_invalid) printf "error: choose 1, 2 or 3\n" >&2 ;;
        hostname_invalid) printf "error: the host name may only contain letters, digits, dots, colons, hyphens, commas and brackets\n" >&2 ;;
        email_invalid) printf "error: --acme-email is not a valid address\n" >&2 ;;
        wrote_env) printf "wrote .env for %s (panel %s, agents %s)\n" "$1" "$2" "$3" ;;
        env_no_acme) printf "error: packaging/core/.env already exists and does not enable ACME. Set TALAIA_ACME_PROVIDER=ovh in it and run again (the installer never replaces an existing .env).\n" >&2 ;;
        acme_checked) printf "acme: checked\n" ;;
        helper_active) printf "update helper: active\n" ;;
        helper_inactive) printf "update helper: not active (%s); see: sudo systemctl status talaia-update.path\n" "$1" ;;
        helper_no_systemd) printf "update helper: not active (systemd is not running); updates from the panel stay off\n" ;;
        helper_not_installed) printf "update helper: not installed (--no-hostd); updates from the panel stay off\n" ;;
        building) printf "building and starting talaia-core (the first build takes a few minutes)\n" ;;
        waiting_core) printf "waiting for the core to answer\n" ;;
        core_no_answer) printf "error: the core did not answer; see: %s\n" "$1" >&2 ;;
        creating_admin) printf "creating the administrator \"%s\" (the password needs at least 12 characters)\n" "$1" ;;
        admin_exists) printf "an administrator already exists; not creating another\n" ;;
        continuing_ca) printf "continuing with the Talaia CA for now.\n" ;;
        final_running) printf "talaia-core is running.\n" ;;
        final_panel) printf "  Panel: %s\n" "$1" ;;
        final_agents) printf "  Agents: %s\n" "$1" ;;
        final_cert_ready) printf "  Certificate: trusted by browsers (ACME).\n" ;;
        final_cert_pending) printf "  Certificate: ACME is configured but not ready yet; until it is, the browser warns about the Talaia CA.\n" ;;
        final_cert_check) printf "  Check: %s\n" "$1" ;;
        final_cert_own1) printf "  The browser warns about the certificate until you trust the Talaia CA:\n" ;;
        final_cert_own2) printf "  download %s and install it as a trusted authority.\n" "$1" ;;
        final_add_server) printf "To add a server, create a token here and run the command it prints on that server:\n" ;;
        final_update_active) printf "To update Talaia, open Settings in the panel; the update helper applies it.\n" ;;
        final_update_inactive) printf "To update Talaia, open Settings in the panel (the update helper is not active; see above).\n" ;;
        final_logs) printf "Logs: %s\n" "$1" ;;
        help)
            cat <<'EOF_EN'

Sets up talaia-core with Docker Compose on this machine: writes .env with a
random database password, builds and starts the stack, and creates the
administrator. Run it from a clone of the repository; it is safe to run again.

Usage:
  packaging/core/install.sh [--hostname <ip-or-name>] [--port <port>] [--agent-port <port>]
                            [--acme delegation|ovh] [--public-ip <IPv4>] [--acme-zone <zone>]
                            [--acme-email <address>] [--no-acme] [--no-acme-check] [--lang <en|es|ca>]
                            [--username <name>] [--no-user] [--no-hostd] [--install-docker]

  --hostname    name or address the core is reached at (asked for in a terminal; default: the first address of this machine)
  --port        port of the panel (default: the first free one from 8443; 80 and 443 are never chosen on their own)
  --agent-port  port of the agents (default: the first free one from 8444)
  --acme        trusted certificate for the panel: delegation (recommended; two DNS records and no
                provider key) or ovh (an OVH API key). In a terminal the installer asks and offers
                the own CA as the third choice.
  --public-ip   public IPv4 for the delegation A record (default: the default route source)
  --acme-zone   DNS zone (the registered domain; default: the last two labels of --hostname)
  --acme-email  contact address for the certificate authority
  --no-acme     do not get a trusted certificate now; keep the Talaia CA
  --no-acme-check  do not check the OVH key with the OVH API (offline installs)
  --lang        language of the installer messages: en, es or ca (default: from LANG; en)
  --username    name of the administrator (default: admin)
  --no-user     do not create the administrator
  --no-hostd    do not install talaia-hostd, the root helper that applies updates requested from the panel
  --install-docker  install Docker with the official script when it is missing (also offered in a
                terminal; it is never installed silently)

Before writing anything it checks the operating system, git, a way to get root, the free disk
space and Docker with its Compose plugin, and explains the fix for whatever is missing. With a
DNS name as --hostname it warns when the name does not resolve, and it prints the firewall
commands for the two ports. It never opens ports.
EOF_EN
            ;;
        *) printf '%s\n' "$key" ;;
    esac
}

msg_es() {
    local key="$1"
    shift
    case "$key" in
        err_lang_value) printf "error: --lang necesita un valor\n" >&2 ;;
        err_lang) printf "error: idioma no admitido %s (admitidos: en, es, ca)\n" "$1" >&2 ;;
        err_hostname_value) printf "error: --hostname necesita un valor\n" >&2 ;;
        err_port_value) printf "error: --port necesita un valor\n" >&2 ;;
        err_agent_port_value) printf "error: --agent-port necesita un valor\n" >&2 ;;
        err_acme_value) printf "error: --acme necesita un valor\n" >&2 ;;
        err_acme_email_value) printf "error: --acme-email necesita un valor\n" >&2 ;;
        err_acme_zone_value) printf "error: --acme-zone necesita un valor\n" >&2 ;;
        err_public_ip_value) printf "error: --public-ip necesita un valor\n" >&2 ;;
        err_username_value) printf "error: --username necesita un valor\n" >&2 ;;
        unknown_option) printf "error: opción desconocida: %s\n" "$1" >&2 ;;
        ports_numbers) printf "error: --port y --agent-port deben ser números\n" >&2 ;;
        ports_differ) printf "error: --port y --agent-port deben ser distintos\n" >&2 ;;
        acme_unsupported) printf "error: proveedor ACME no admitido %s (admitidos: delegation, ovh)\n" "$1" >&2 ;;
        acme_no_conflict) printf "error: usa --acme o --no-acme, no ambos\n" >&2 ;;
        no_free_port) printf "error: no hay ningún puerto libre desde %s; pasa --port y --agent-port\n" "$1" >&2 ;;
        dns_no_getent) printf "aviso: no se puede comprobar el registro DNS de %s (getent no está disponible)\n" "$1" >&2 ;;
        dns_mismatch) printf "aviso: %s resuelve a %s, que no es una dirección de esta máquina (%s). Es normal detrás de un NAT del proveedor; si no, revisa el registro DNS.\n" "$1" "$2" "$3" >&2 ;;
        dns_unresolved) printf "aviso: %s todavía no resuelve. Crea un registro DNS A/AAAA que apunte a este servidor; los navegadores no encontrarán el panel hasta que exista.\n" "$1" >&2 ;;
        fw_note_ufw) printf "nota: ufw está instalado; ejecuta 'sudo ufw status' para ver si está activo\n" >&2 ;;
        fw_note_firewalld) printf "nota: firewalld está instalado; ejecuta 'sudo firewall-cmd --state' para ver si está en marcha\n" >&2 ;;
        fw_ufw_active) printf "ufw está activo. Abre los dos puertos con:\n" ;;
        fw_firewalld_active) printf "firewalld está activo. Abre los dos puertos con:\n" ;;
        fw_none) printf "No se ha detectado ufw ni firewalld activos.\n" ;;
        fw_provider) printf "Abre también TCP %s y %s en el cortafuegos o el grupo de seguridad de tu proveedor; el instalador nunca abre puertos.\n" "$1" "$2" ;;
        pf_os_missing) printf "aviso: no se pudo leer /etc/os-release; este instalador se prueba en Debian y Ubuntu\n" >&2 ;;
        pf_os_other) printf "aviso: %s no es Debian ni Ubuntu; este instalador se prueba en esos sistemas y los siguientes pasos pueden variar\n" "$1" >&2 ;;
        pf_git) printf "error: git es necesario para registrar el commit que usan las actualizaciones. Instálalo con '%s' (o el equivalente de tu distribución) y vuelve a ejecutar.\n" "$1" >&2 ;;
        pf_curl) printf "error: curl es necesario para esperar al núcleo y comprobarlo. Instálalo con '%s' (o el equivalente de tu distribución) y vuelve a ejecutar.\n" "$1" >&2 ;;
        pf_root) printf "error: este instalador necesita root o sudo para instalar el ayudante de actualizaciones y Docker. Ejecútalo como root o instala sudo.\n" >&2 ;;
        pf_sudo_note) printf "nota: sudo te pedirá la contraseña cuando la necesite\n" ;;
        pf_disk_read) printf "aviso: no se pudo leer el espacio libre de %s\n" "$1" >&2 ;;
        pf_disk_low) printf "error: quedan menos de 2 GiB libres en %s. Libera espacio (por ejemplo: docker system prune -af, o borra imágenes y compilaciones antiguas) y vuelve a ejecutar.\n" "$1" >&2 ;;
        pf_disk_warn) printf "aviso: quedan menos de 5 GiB libres en %s; la primera compilación necesita espacio para imágenes y capas\n" "$1" >&2 ;;
        pf_docker_missing) printf "Docker no está instalado. Talaia ejecuta el núcleo y su base de datos como contenedores.\n" ;;
        pf_docker_official) printf "Instalación oficial: %s\n" "$1" ;;
        pf_docker_installing) printf "instalando Docker con el script oficial\n" ;;
        pf_docker_failed) printf "error: la instalación de Docker ha fallado. Instálalo con '%s' y vuelve a ejecutar.\n" "$1" >&2 ;;
        pf_docker_required) printf "error: Docker es necesario. Instálalo con '%s' (o pasa --install-docker) y vuelve a ejecutar.\n" "$1" >&2 ;;
        pf_docker_still) printf "error: Docker sigue sin estar disponible. Abre una shell nueva y vuelve a ejecutar el instalador, o instálalo con '%s'.\n" "$1" >&2 ;;
        pf_docker_sudo) printf "nota: este usuario todavía no puede hablar con Docker; el instalador usará sudo. Cierra la sesión y vuelve a entrar para dejar de usar sudo.\n" ;;
        pf_compose) printf "error: falta el plugin Docker Compose. Instálalo con '%s' (en algunos sistemas el paquete es '%s') y vuelve a ejecutar.\n" "$1" "$2" >&2 ;;
        pf_ok) printf "comprobación previa: ok\n" ;;
        ask_docker) printf "¿Instalar Docker ahora con el script oficial? (sí/no)" ;;
        zone_missing) printf "error: pasa --acme-zone <zona> (el dominio registrado) y vuelve a ejecutar.\n" >&2 ;;
        zone_invalid) printf "error: --acme-zone solo puede contener letras, dígitos, puntos y guiones\n" >&2 ;;
        zone_prompt) printf "Zona DNS (el dominio registrado; para nombres como .co.uk escribe el dominio registrado)" ;;
        hostname_prompt) printf "Nombre o dirección desde la que se llega al núcleo" ;;
        email_prompt) printf "Dirección de contacto para la autoridad de certificación (opcional)" ;;
        cleanup_staging) printf "aviso: la ejecución ha terminado antes de producción; ACME está desactivado y se ha quitado staging.\n" >&2 ;;
        finish_later) printf "Para terminarlo más tarde: %s\n" "$1" ;;
        wrote_acme) printf "se ha escrito .env.acme\n" ;;
        keep_acme) printf "se conserva el .env.acme existente\n" ;;
        ovh_sha1) printf "error: sha1sum es necesario para firmar la petición a OVH (paquete coreutils).\n" >&2 ;;
        ovh_unreachable) printf "OVH: no se pudo contactar con OVH en %s\n" "$1" >&2 ;;
        ovh_unreachable_http) printf "OVH: no se pudo contactar con OVH en %s (HTTP %s)\n" "$1" "$2" >&2 ;;
        ovh_invalid_time) printf "OVH: OVH devolvió una hora no válida\n" >&2 ;;
        ovh_key_ok) printf "Clave de OVH: correcta\n" ;;
        ovh_missing_right) printf "Clave de OVH: falta el derecho: %s\n" "$1" ;;
        ovh_rejected) printf "OVH ha rechazado la clave: %s\n" "$1" >&2 ;;
        ovh_not_validated) printf "OVH: la clave se ha creado pero aún no está validada. Abre la página de validación que muestra OVH (o la página de claves de la API), termina la validación y vuelve a ejecutar el instalador.\n" >&2 ;;
        ovh_rejected_http) printf "OVH ha rechazado la clave (HTTP %s): %s\n" "$1" "$2" >&2 ;;
        acme_file_incomplete) printf "error: .env.acme está incompleto; necesita OVH_APPLICATION_KEY, OVH_APPLICATION_SECRET y OVH_CONSUMER_KEY.\n" >&2 ;;
        acme_file_chars) printf "error: .env.acme contiene caracteres inesperados; edita los tres valores de OVH y vuelve a ejecutar.\n" >&2 ;;
        ovh_region_invalid) printf "error: la región de OVH debe ser ovh-eu u ovh-ca (recibido: %s)\n" "$1" >&2 ;;
        ovh_region_prompt) printf "Región de OVH (ovh-eu u ovh-ca)" ;;
        ovh_create_key) printf "Crea una clave de la API de OVH para la zona %s con estos tres derechos:\n" "$1" ;;
        ovh_guide) printf "Guía: %s (también docs/es/ovh-dns.md y docs/ca/ovh-dns.md).\n" "$1" ;;
        ovh_enter_values) printf "Pulsa Enter cuando tengas los tres valores: " ;;
        ovh_app_key) printf "Clave de aplicación de OVH: " ;;
        ovh_secret) printf "Secreto de aplicación de OVH: " ;;
        ovh_consumer) printf "Clave de consumidor de OVH: " ;;
        ovh_missing_env) printf "error: define OVH_APPLICATION_KEY, OVH_APPLICATION_SECRET y OVH_CONSUMER_KEY (o ejecuta en un terminal)\n" >&2 ;;
        ovh_bad_chars) printf "error: los valores de la clave de OVH contienen caracteres inesperados; cópialos otra vez.\n" >&2 ;;
        ovh_skip) printf "se omite la comprobación de OVH (--no-acme-check)\n" ;;
        ovh_continue_unchecked) printf "aviso: se continúa sin comprobar la clave de OVH\n" ;;
        ovh_validate_failed) printf "error: no se ha podido validar la clave de OVH; corrígela o pasa --no-acme-check (instalaciones sin conexión).\n" >&2 ;;
        ovh_enter_again) printf "vuelve a introducir los tres valores\n" ;;
        ovh_off) printf "se continúa con la CA de Talaia; ACME está desactivado.\n" ;;
        ovh_turned_off) printf "ACME desactivado en packaging/core/.env.\n" ;;
        ovh_abort) printf "se aborta sin construir nada.\n" >&2 ;;
        ovh_menu) printf "La comprobación de OVH ha fallado: [1] volver a introducir las claves, [2] continuar sin certificado de confianza (CA propia, ACME desactivado), [3] abortar" ;;
        ovh_reach_prompt) printf "No se pudo contactar con OVH. ¿Continuar sin validar la clave? (sí/no)" ;;
        ip_not_found) printf "error: no se pudo encontrar la IPv4 pública; pasa --public-ip <IPv4>.\n" >&2 ;;
        ip_invalid) printf "error: %s no es una dirección IPv4 válida; pasa --public-ip <IPv4>.\n" "$1" >&2 ;;
        ip_nat) printf "Este servidor está detrás de un NAT: %s es una dirección privada.\n" "$1" >&2 ;;
        ip_nat_fix) printf "error: pasa la dirección IPv4 pública con --public-ip <IPv4>.\n" >&2 ;;
        ip_prompt) printf "Dirección IPv4 pública de este servidor" ;;
        ss_missing) printf "aviso: ss no está disponible; no se puede comprobar si el puerto 53 está libre.\n" >&2 ;;
        port53_own) printf "el puerto 53 lo ocupa el contenedor acme-dns de esta instalación; se conserva.\n" ;;
        port53_conflict) printf "error: ya hay algo escuchando en el puerto 53 en %s%s.\n" "$1" "$2" >&2 ;;
        port53_need) printf "acme-dns necesita UDP y TCP en el puerto 53 de la IPv4 pública %s.\n" "$1" >&2 ;;
        env_key_invalid) printf "error: clave de .env no válida: %s\n" "$1" >&2 ;;
        env_value_invalid) printf "error: el valor de %s contiene un espacio, comillas, un salto de línea o |.\n" "$1" >&2 ;;
        acme_off) printf "ACME está desactivado; el panel usa la CA de Talaia por ahora.\n" ;;
        rec_outside) printf "aviso: %s no está dentro de %s; revisa la zona.\n" "$1" "$2" >&2 ;;
        rec_create) printf "Crea estos dos registros en la zona DNS de %s (en tu proveedor de DNS):\n" "$1" ;;
        rec_type) printf "Tipo" ;;
        rec_name) printf "Nombre" ;;
        rec_value) printf "Valor" ;;
        rec_no_aaaa) printf "No crees un registro AAAA para %s: el servidor de desafíos solo responde por IPv4.\n" "$1" ;;
        rec_guide) printf "Guía: %s (también docs/es y docs/ca).\n" "$1" ;;
        dfw_note_ufw) printf "nota: ufw está instalado; ejecuta 'sudo ufw status' para ver si está activo.\n" >&2 ;;
        dfw_note_firewalld) printf "nota: firewalld está instalado; ejecuta 'sudo firewall-cmd --state' para ver si está en marcha.\n" >&2 ;;
        dfw_ports) printf "La delegación necesita estos puertos abiertos:\n" ;;
        dfw_ports_detail) printf "  TCP %s (panel), TCP %s (agentes), UDP 53 y TCP 53 (acme-dns).\n" "$1" "$2" ;;
        dfw_ufw_question) printf "¿Abrir estos puertos en ufw ahora? (sí/no)" ;;
        dfw_firewalld_question) printf "¿Abrir estos puertos en firewalld ahora? (sí/no)" ;;
        dfw_opened_ufw) printf "puertos abiertos en ufw\n" ;;
        dfw_opened_firewalld) printf "puertos abiertos en firewalld\n" ;;
        dfw_ufw_active) printf "ufw está activo. Ábrelos con:\n" ;;
        dfw_firewalld_active) printf "firewalld está activo. Ábrelos con:\n" ;;
        dfw_installed_inactive) printf "ufw o firewalld están instalados pero no activos.\n" ;;
        dfw_none) printf "No se ha detectado ufw ni firewalld activos.\n" ;;
        dfw_provider) printf "Permite también TCP %s, TCP %s, UDP 53 y TCP 53 en el cortafuegos o el grupo de seguridad de tu proveedor.\n" "$1" "$2" ;;
        dfw_never) printf "El instalador nunca abre un puerto en el cortafuegos del proveedor.\n" ;;
        dfw_enter) printf "Pulsa Enter cuando existan los registros y los puertos estén abiertos" ;;
        del_needs_dns) printf "error: la delegación necesita un nombre DNS en --hostname (no una dirección IP).\n" >&2 ;;
        del_use_alt) printf "Usa --acme ovh o --no-acme, o instala con --hostname talaia.example.com.\n" >&2 ;;
        del_port53_in_use) printf "El puerto 53 ya está en uso, así que acme-dns no puede usar la IPv4 pública.\n" ;;
        del_choice_ovh) printf "  1) usar una clave de la API de OVH\n" ;;
        del_choice_off) printf "  2) continuar sin certificado de confianza por ahora (CA propia de Talaia)\n" ;;
        del_choice_quit) printf "  3) salir\n" ;;
        del_choice) printf "Elección" ;;
        del_stop) printf "error: detén el proceso que escucha en el puerto 53, o usa --acme ovh o --no-acme.\n" >&2 ;;
        check_attempt) printf "comprobando la delegación con los resolutores públicos (intento %s de %s)\n" "$1" "$2" ;;
        check_wait) printf "Los cambios de DNS pueden tardar unos minutos.\n" ;;
        check_menu) printf "Enter: comprobar otra vez, o: continuar sin certificado de confianza, q: salir" ;;
        check_unknown) printf "opción desconocida; se comprueba otra vez\n" >&2 ;;
        check_gave_up) printf "error: la comprobación de la delegación no ha pasado tras %s intentos.\n" "$1" >&2 ;;
        staging_restart) printf "reiniciando el núcleo para pedir el certificado de staging\n" ;;
        staging_ok) printf "certificado de staging: ok (la delegación funciona desde internet)\n" ;;
        staging_failed) printf "error: el certificado de staging no ha llegado en cuatro minutos.\n" >&2 ;;
        staging_warning) printf "último aviso de ACME: %s\n" "$1" >&2 ;;
        staging_menu) printf "Staging ha fallado: [1] reintentar, [2] continuar sin certificado de confianza, [3] salir" ;;
        hostd_no_systemd) printf "aviso: no hay systemd; las actualizaciones desde el panel quedan desactivadas\n" >&2 ;;
        hostd_not_git) printf "aviso: %s no es un clon de git; las actualizaciones desde el panel quedan desactivadas\n" "$1" >&2 ;;
        hostd_bad_path) printf "aviso: la ruta %s tiene caracteres que el ayudante rechaza; las actualizaciones desde el panel quedan desactivadas\n" "$1" >&2 ;;
        hostd_installing) printf "instalando el ayudante de actualizaciones (talaia-hostd) para el clon en %s\n" "$1" ;;
        self_panel_ok) printf "panel: ok (%s)\n" "$1" ;;
        self_panel_failed) printf "panel: FALLÓ (%s); consulta: %s\n" "$1" "$2" ;;
        self_agents_ok) printf "agentes: ok (%s)\n" "$1" ;;
        self_agents_failed) printf "agentes: FALLÓ (%s); consulta: %s\n" "$1" "$2" ;;
        acme_obtained) printf "Certificado ACME: obtenido\n" ;;
        acme_pending_warning) printf "Certificado ACME: todavía pendiente; último aviso: %s\n" "$1" ;;
        acme_pending) printf "Certificado ACME: todavía pendiente (el núcleo sigue intentándolo; consulta: %s)\n" "$1" ;;
        keep_env) printf "se conserva el .env existente\n" ;;
        no_address) printf "error: no se pudo encontrar una dirección para esta máquina; pasa --hostname\n" >&2 ;;
        port_busy) printf "error: el puerto %s ya está en uso; elige otro con --port\n" "$1" >&2 ;;
        agent_port_busy) printf "error: el puerto %s ya está en uso; elige otro con --agent-port\n" "$1" >&2 ;;
        cert_choice_title) printf "¿Qué certificado debe usar el panel?\n" ;;
        cert_choice_1) printf "  1) automático, con dos registros DNS (recomendado)\n" ;;
        cert_choice_2) printf "  2) con una clave de la API de OVH\n" ;;
        cert_choice_3) printf "  3) ninguno por ahora (CA propia de Talaia)\n" ;;
        cert_choice) printf "Elección" ;;
        cert_choice_invalid) printf "error: elige 1, 2 o 3\n" >&2 ;;
        hostname_invalid) printf "error: el nombre solo puede contener letras, dígitos, puntos, dos puntos, guiones, comas y corchetes\n" >&2 ;;
        email_invalid) printf "error: --acme-email no es una dirección válida\n" >&2 ;;
        wrote_env) printf "se ha escrito .env para %s (panel %s, agentes %s)\n" "$1" "$2" "$3" ;;
        env_no_acme) printf "error: packaging/core/.env ya existe y no activa ACME. Define TALAIA_ACME_PROVIDER=ovh en él y vuelve a ejecutar (el instalador nunca sustituye un .env existente).\n" >&2 ;;
        acme_checked) printf "acme: comprobado\n" ;;
        helper_active) printf "ayudante de actualizaciones: activo\n" ;;
        helper_inactive) printf "ayudante de actualizaciones: no activo (%s); consulta: sudo systemctl status talaia-update.path\n" "$1" ;;
        helper_no_systemd) printf "ayudante de actualizaciones: no activo (systemd no está en marcha); las actualizaciones desde el panel quedan desactivadas\n" ;;
        helper_not_installed) printf "ayudante de actualizaciones: no instalado (--no-hostd); las actualizaciones desde el panel quedan desactivadas\n" ;;
        building) printf "construyendo e iniciando talaia-core (la primera compilación tarda unos minutos)\n" ;;
        waiting_core) printf "esperando a que el núcleo responda\n" ;;
        core_no_answer) printf "error: el núcleo no ha respondido; consulta: %s\n" "$1" >&2 ;;
        creating_admin) printf "creando el administrador \"%s\" (la contraseña necesita al menos 12 caracteres)\n" "$1" ;;
        admin_exists) printf "ya existe un administrador; no se crea otro\n" ;;
        continuing_ca) printf "se continúa con la CA de Talaia por ahora.\n" ;;
        final_running) printf "talaia-core está en marcha.\n" ;;
        final_panel) printf "  Panel: %s\n" "$1" ;;
        final_agents) printf "  Agentes: %s\n" "$1" ;;
        final_cert_ready) printf "  Certificado: de confianza para los navegadores (ACME).\n" ;;
        final_cert_pending) printf "  Certificado: ACME está configurado pero todavía no está listo; hasta entonces, el navegador avisa de la CA de Talaia.\n" ;;
        final_cert_check) printf "  Comprueba: %s\n" "$1" ;;
        final_cert_own1) printf "  El navegador avisará del certificado hasta que confíes en la CA de Talaia:\n" ;;
        final_cert_own2) printf "  descarga %s e instálalo como autoridad de confianza.\n" "$1" ;;
        final_add_server) printf "Para añadir un servidor, crea un token aquí y ejecuta en ese servidor la orden que imprime:\n" ;;
        final_update_active) printf "Para actualizar Talaia, abre Ajustes en el panel; el ayudante de actualizaciones lo aplica.\n" ;;
        final_update_inactive) printf "Para actualizar Talaia, abre Ajustes en el panel (el ayudante de actualizaciones no está activo; mira arriba).\n" ;;
        final_logs) printf "Registros: %s\n" "$1" ;;
        help)
            cat <<'EOF_ES'

Configura talaia-core con Docker Compose en esta máquina: escribe .env con una
contraseña aleatoria para la base de datos, construye e inicia la pila y crea
el administrador. Ejecútalo desde un clon del repositorio; se puede repetir.

Uso:
  packaging/core/install.sh [--hostname <ip-o-nombre>] [--port <puerto>] [--agent-port <puerto>]
                            [--acme delegation|ovh] [--public-ip <IPv4>] [--acme-zone <zona>]
                            [--acme-email <dirección>] [--no-acme] [--no-acme-check] [--lang <en|es|ca>]
                            [--username <nombre>] [--no-user] [--no-hostd] [--install-docker]

  --hostname    nombre o dirección desde la que se llega al núcleo (se pregunta en un terminal; por defecto: la primera dirección de esta máquina)
  --port        puerto del panel (por defecto: el primero libre desde 8443; nunca elige 80 ni 443 por su cuenta)
  --agent-port  puerto de los agentes (por defecto: el primero libre desde 8444)
  --acme        certificado de confianza para el panel: delegation (recomendado; dos registros DNS y
                ninguna clave de proveedor) u ovh (una clave de la API de OVH). En un terminal el
                instalador pregunta y ofrece la CA propia como tercera opción.
  --public-ip   IPv4 pública para el registro A de la delegación (por defecto: el origen de la ruta por defecto)
  --acme-zone   zona DNS (el dominio registrado; por defecto: las dos últimas etiquetas de --hostname)
  --acme-email  dirección de contacto para la autoridad de certificación
  --no-acme     no obtener un certificado de confianza ahora; usar la CA de Talaia
  --no-acme-check  no comprobar la clave de OVH con la API de OVH (instalaciones sin conexión)
  --lang        idioma de los mensajes del instalador: en, es o ca (por defecto: desde LANG; en)
  --username    nombre del administrador (por defecto: admin)
  --no-user     no crear el administrador
  --no-hostd    no instalar talaia-hostd, el ayudante de root que aplica las actualizaciones pedidas desde el panel
  --install-docker  instalar Docker con el script oficial si falta (también se ofrece en un
                terminal; nunca se instala en silencio)

Antes de escribir nada comprueba el sistema operativo, git, una forma de obtener root, el espacio
libre y Docker con su plugin Compose, y explica cómo arreglar lo que falte. Con un nombre DNS en
--hostname avisa si el nombre no resuelve, e imprime las órdenes de cortafuegos para los dos
puertos. Nunca abre puertos.
EOF_ES
            ;;
        *) return 1 ;;
    esac
}

msg_ca() {
    local key="$1"
    shift
    case "$key" in
        err_lang_value) printf "error: --lang necessita un valor\n" >&2 ;;
        err_lang) printf "error: idioma no admès %s (admesos: en, es, ca)\n" "$1" >&2 ;;
        err_hostname_value) printf "error: --hostname necessita un valor\n" >&2 ;;
        err_port_value) printf "error: --port necessita un valor\n" >&2 ;;
        err_agent_port_value) printf "error: --agent-port necessita un valor\n" >&2 ;;
        err_acme_value) printf "error: --acme necessita un valor\n" >&2 ;;
        err_acme_email_value) printf "error: --acme-email necessita un valor\n" >&2 ;;
        err_acme_zone_value) printf "error: --acme-zone necessita un valor\n" >&2 ;;
        err_public_ip_value) printf "error: --public-ip necessita un valor\n" >&2 ;;
        err_username_value) printf "error: --username necessita un valor\n" >&2 ;;
        unknown_option) printf "error: opció desconeguda: %s\n" "$1" >&2 ;;
        ports_numbers) printf "error: --port i --agent-port han de ser nombres\n" >&2 ;;
        ports_differ) printf "error: --port i --agent-port han de ser diferents\n" >&2 ;;
        acme_unsupported) printf "error: proveïdor ACME no admès %s (admesos: delegation, ovh)\n" "$1" >&2 ;;
        acme_no_conflict) printf "error: usa --acme o --no-acme, no tots dos\n" >&2 ;;
        no_free_port) printf "error: no hi ha cap port lliure des de %s; passa --port i --agent-port\n" "$1" >&2 ;;
        dns_no_getent) printf "avís: no es pot comprovar el registre DNS de %s (getent no està disponible)\n" "$1" >&2 ;;
        dns_mismatch) printf "avís: %s resol a %s, que no és una adreça d'aquesta màquina (%s). És normal darrere d'un NAT del proveïdor; si no, revisa el registre DNS.\n" "$1" "$2" "$3" >&2 ;;
        dns_unresolved) printf "avís: %s encara no resol. Crea un registre DNS A/AAAA que apunti a aquest servidor; els navegadors no trobaran el panell fins que existeixi.\n" "$1" >&2 ;;
        fw_note_ufw) printf "nota: ufw està instal·lat; executa 'sudo ufw status' per veure si està actiu\n" >&2 ;;
        fw_note_firewalld) printf "nota: firewalld està instal·lat; executa 'sudo firewall-cmd --state' per veure si funciona\n" >&2 ;;
        fw_ufw_active) printf "ufw està actiu. Obre els dos ports amb:\n" ;;
        fw_firewalld_active) printf "firewalld està actiu. Obre els dos ports amb:\n" ;;
        fw_none) printf "No s'ha detectat ufw ni firewalld actius.\n" ;;
        fw_provider) printf "Obre també TCP %s i %s al tallafoc o al grup de seguretat del teu proveïdor; l'instal·lador mai obre ports.\n" "$1" "$2" ;;
        pf_os_missing) printf "avís: no s'ha pogut llegir /etc/os-release; aquest instal·lador es prova a Debian i Ubuntu\n" >&2 ;;
        pf_os_other) printf "avís: %s no és Debian ni Ubuntu; aquest instal·lador es prova en aquests sistemes i els passos següents poden variar\n" "$1" >&2 ;;
        pf_git) printf "error: cal git per registrar el commit que fan servir les actualitzacions. Instal·la'l amb '%s' (o l'equivalent de la teva distribució) i torna a executar.\n" "$1" >&2 ;;
        pf_curl) printf "error: cal curl per esperar el nucli i comprovar-lo. Instal·la'l amb '%s' (o l'equivalent de la teva distribució) i torna a executar.\n" "$1" >&2 ;;
        pf_root) printf "error: aquest instal·lador necessita root o sudo per instal·lar l'ajudant d'actualitzacions i Docker. Executa'l com a root o instal·la sudo.\n" >&2 ;;
        pf_sudo_note) printf "nota: sudo et demanarà la contrasenya quan calgui\n" ;;
        pf_disk_read) printf "avís: no s'ha pogut llegir l'espai lliure de %s\n" "$1" >&2 ;;
        pf_disk_low) printf "error: queden menys de 2 GiB lliures a %s. Allibera espai (per exemple: docker system prune -af, o esborra imatges i compilacions antigues) i torna a executar.\n" "$1" >&2 ;;
        pf_disk_warn) printf "avís: queden menys de 5 GiB lliures a %s; la primera compilació necessita espai per a imatges i capes\n" "$1" >&2 ;;
        pf_docker_missing) printf "Docker no està instal·lat. Talaia executa el nucli i la seva base de dades com a contenidors.\n" ;;
        pf_docker_official) printf "Instal·lació oficial: %s\n" "$1" ;;
        pf_docker_installing) printf "instal·lant Docker amb l'script oficial\n" ;;
        pf_docker_failed) printf "error: la instal·lació de Docker ha fallat. Instal·la'l amb '%s' i torna a executar.\n" "$1" >&2 ;;
        pf_docker_required) printf "error: cal Docker. Instal·la'l amb '%s' (o passa --install-docker) i torna a executar.\n" "$1" >&2 ;;
        pf_docker_still) printf "error: Docker encara no està disponible. Obre una shell nova i torna a executar l'instal·lador, o instal·la'l amb '%s'.\n" "$1" >&2 ;;
        pf_docker_sudo) printf "nota: aquest usuari encara no pot parlar amb Docker; l'instal·lador farà servir sudo. Tanca la sessió i torna a entrar per deixar de fer servir sudo.\n" ;;
        pf_compose) printf "error: falta el connector Docker Compose. Instal·la'l amb '%s' (en alguns sistemes el paquet és '%s') i torna a executar.\n" "$1" "$2" >&2 ;;
        pf_ok) printf "comprovació prèvia: ok\n" ;;
        ask_docker) printf "Vols instal·lar Docker ara amb l'script oficial? (sí/no)" ;;
        zone_missing) printf "error: passa --acme-zone <zona> (el domini registrat) i torna a executar.\n" >&2 ;;
        zone_invalid) printf "error: --acme-zone només pot contenir lletres, dígits, punts i guions\n" >&2 ;;
        zone_prompt) printf "Zona DNS (el domini registrat; per a noms com .co.uk escriu el domini registrat)" ;;
        hostname_prompt) printf "Nom o adreça des de la qual s'arriba al nucli" ;;
        email_prompt) printf "Adreça de contacte per a l'autoritat de certificació (opcional)" ;;
        cleanup_staging) printf "avís: l'execució ha acabat abans de producció; ACME està desactivat i s'ha tret staging.\n" >&2 ;;
        finish_later) printf "Per acabar-ho més tard: %s\n" "$1" ;;
        wrote_acme) printf "s'ha escrit .env.acme\n" ;;
        keep_acme) printf "es conserva el .env.acme existent\n" ;;
        ovh_sha1) printf "error: cal sha1sum per signar la petició a OVH (paquet coreutils).\n" >&2 ;;
        ovh_unreachable) printf "OVH: no s'ha pogut contactar amb OVH a %s\n" "$1" >&2 ;;
        ovh_unreachable_http) printf "OVH: no s'ha pogut contactar amb OVH a %s (HTTP %s)\n" "$1" "$2" >&2 ;;
        ovh_invalid_time) printf "OVH: OVH ha retornat una hora no vàlida\n" >&2 ;;
        ovh_key_ok) printf "Clau d'OVH: correcta\n" ;;
        ovh_missing_right) printf "Clau d'OVH: falta el dret: %s\n" "$1" ;;
        ovh_rejected) printf "OVH ha rebutjat la clau: %s\n" "$1" >&2 ;;
        ovh_not_validated) printf "OVH: la clau s'ha creat però encara no està validada. Obre la pàgina de validació que mostra OVH (o la pàgina de claus de l'API), acaba la validació i torna a executar l'instal·lador.\n" >&2 ;;
        ovh_rejected_http) printf "OVH ha rebutjat la clau (HTTP %s): %s\n" "$1" "$2" >&2 ;;
        acme_file_incomplete) printf "error: .env.acme està incomplet; necessita OVH_APPLICATION_KEY, OVH_APPLICATION_SECRET i OVH_CONSUMER_KEY.\n" >&2 ;;
        acme_file_chars) printf "error: .env.acme conté caràcters inesperats; edita els tres valors d'OVH i torna a executar.\n" >&2 ;;
        ovh_region_invalid) printf "error: la regió d'OVH ha de ser ovh-eu o ovh-ca (rebut: %s)\n" "$1" >&2 ;;
        ovh_region_prompt) printf "Regió d'OVH (ovh-eu o ovh-ca)" ;;
        ovh_create_key) printf "Crea una clau de l'API d'OVH per a la zona %s amb aquests tres drets:\n" "$1" ;;
        ovh_guide) printf "Guia: %s (també docs/es/ovh-dns.md i docs/ca/ovh-dns.md).\n" "$1" ;;
        ovh_enter_values) printf "Prem Enter quan tinguis els tres valors: " ;;
        ovh_app_key) printf "Clau d'aplicació d'OVH: " ;;
        ovh_secret) printf "Secret d'aplicació d'OVH: " ;;
        ovh_consumer) printf "Clau de consumidor d'OVH: " ;;
        ovh_missing_env) printf "error: defineix OVH_APPLICATION_KEY, OVH_APPLICATION_SECRET i OVH_CONSUMER_KEY (o executa en un terminal)\n" >&2 ;;
        ovh_bad_chars) printf "error: els valors de la clau d'OVH contenen caràcters inesperats; torna'ls a copiar.\n" >&2 ;;
        ovh_skip) printf "s'omet la comprovació d'OVH (--no-acme-check)\n" ;;
        ovh_continue_unchecked) printf "avís: es continua sense comprovar la clau d'OVH\n" ;;
        ovh_validate_failed) printf "error: no s'ha pogut validar la clau d'OVH; corregeix-la o passa --no-acme-check (instal·lacions sense connexió).\n" >&2 ;;
        ovh_enter_again) printf "torna a introduir els tres valors\n" ;;
        ovh_off) printf "es continua amb la CA de Talaia; ACME està desactivat.\n" ;;
        ovh_turned_off) printf "ACME desactivat a packaging/core/.env.\n" ;;
        ovh_abort) printf "s'avorta sense construir res.\n" >&2 ;;
        ovh_menu) printf "La comprovació d'OVH ha fallat: [1] tornar a introduir les claus, [2] continuar sense certificat de confiança (CA pròpia, ACME desactivat), [3] avortar" ;;
        ovh_reach_prompt) printf "No s'ha pogut contactar amb OVH. Vols continuar sense validar la clau? (sí/no)" ;;
        ip_not_found) printf "error: no s'ha pogut trobar la IPv4 pública; passa --public-ip <IPv4>.\n" >&2 ;;
        ip_invalid) printf "error: %s no és una adreça IPv4 vàlida; passa --public-ip <IPv4>.\n" "$1" >&2 ;;
        ip_nat) printf "Aquest servidor és darrere d'un NAT: %s és una adreça privada.\n" "$1" >&2 ;;
        ip_nat_fix) printf "error: passa l'adreça IPv4 pública amb --public-ip <IPv4>.\n" >&2 ;;
        ip_prompt) printf "Adreça IPv4 pública d'aquest servidor" ;;
        ss_missing) printf "avís: ss no està disponible; no es pot comprovar si el port 53 és lliure.\n" >&2 ;;
        port53_own) printf "el port 53 l'ocupa el contenidor acme-dns d'aquesta instal·lació; es conserva.\n" ;;
        port53_conflict) printf "error: ja hi ha alguna cosa escoltant al port 53 a %s%s.\n" "$1" "$2" >&2 ;;
        port53_need) printf "acme-dns necessita UDP i TCP al port 53 de la IPv4 pública %s.\n" "$1" >&2 ;;
        env_key_invalid) printf "error: clau de .env no vàlida: %s\n" "$1" >&2 ;;
        env_value_invalid) printf "error: el valor de %s conté un espai, cometes, un salt de línia o |.\n" "$1" >&2 ;;
        acme_off) printf "ACME està desactivat; el panell fa servir la CA de Talaia per ara.\n" ;;
        rec_outside) printf "avís: %s no és dins de %s; revisa la zona.\n" "$1" "$2" >&2 ;;
        rec_create) printf "Crea aquests dos registres a la zona DNS de %s (al teu proveïdor de DNS):\n" "$1" ;;
        rec_type) printf "Tipus" ;;
        rec_name) printf "Nom" ;;
        rec_value) printf "Valor" ;;
        rec_no_aaaa) printf "No crees un registre AAAA per a %s: el servidor de reptes només respon per IPv4.\n" "$1" ;;
        rec_guide) printf "Guia: %s (també docs/es i docs/ca).\n" "$1" ;;
        dfw_note_ufw) printf "nota: ufw està instal·lat; executa 'sudo ufw status' per veure si està actiu.\n" >&2 ;;
        dfw_note_firewalld) printf "nota: firewalld està instal·lat; executa 'sudo firewall-cmd --state' per veure si funciona.\n" >&2 ;;
        dfw_ports) printf "La delegació necessita aquests ports oberts:\n" ;;
        dfw_ports_detail) printf "  TCP %s (panell), TCP %s (agents), UDP 53 i TCP 53 (acme-dns).\n" "$1" "$2" ;;
        dfw_ufw_question) printf "Vols obrir aquests ports a ufw ara? (sí/no)" ;;
        dfw_firewalld_question) printf "Vols obrir aquests ports a firewalld ara? (sí/no)" ;;
        dfw_opened_ufw) printf "ports oberts a ufw\n" ;;
        dfw_opened_firewalld) printf "ports oberts a firewalld\n" ;;
        dfw_ufw_active) printf "ufw està actiu. Obre'ls amb:\n" ;;
        dfw_firewalld_active) printf "firewalld està actiu. Obre'ls amb:\n" ;;
        dfw_installed_inactive) printf "ufw o firewalld estan instal·lats però no actius.\n" ;;
        dfw_none) printf "No s'ha detectat ufw ni firewalld actius.\n" ;;
        dfw_provider) printf "Permet també TCP %s, TCP %s, UDP 53 i TCP 53 al tallafoc o al grup de seguretat del teu proveïdor.\n" "$1" "$2" ;;
        dfw_never) printf "L'instal·lador mai obre un port al tallafoc del proveïdor.\n" ;;
        dfw_enter) printf "Prem Enter quan existeixin els registres i els ports estiguin oberts" ;;
        del_needs_dns) printf "error: la delegació necessita un nom DNS a --hostname (no una adreça IP).\n" >&2 ;;
        del_use_alt) printf "Usa --acme ovh o --no-acme, o instal·la amb --hostname talaia.example.com.\n" >&2 ;;
        del_port53_in_use) printf "El port 53 ja està en ús, així que acme-dns no pot fer servir la IPv4 pública.\n" ;;
        del_choice_ovh) printf "  1) usar una clau de l'API d'OVH\n" ;;
        del_choice_off) printf "  2) continuar sense certificat de confiança per ara (CA pròpia de Talaia)\n" ;;
        del_choice_quit) printf "  3) sortir\n" ;;
        del_choice) printf "Opció" ;;
        del_stop) printf "error: atura el procés que escolta al port 53, o usa --acme ovh o --no-acme.\n" >&2 ;;
        check_attempt) printf "comprovant la delegació amb els resolutors públics (intent %s de %s)\n" "$1" "$2" ;;
        check_wait) printf "Els canvis de DNS poden trigar uns minuts.\n" ;;
        check_menu) printf "Enter: comprovar de nou, o: continuar sense certificat de confiança, q: sortir" ;;
        check_unknown) printf "opció desconeguda; es comprova de nou\n" >&2 ;;
        check_gave_up) printf "error: la comprovació de la delegació no ha passat després de %s intents.\n" "$1" >&2 ;;
        staging_restart) printf "reiniciant el nucli per demanar el certificat de staging\n" ;;
        staging_ok) printf "certificat de staging: ok (la delegació funciona des d'internet)\n" ;;
        staging_failed) printf "error: el certificat de staging no ha arribat en quatre minuts.\n" >&2 ;;
        staging_warning) printf "últim avís d'ACME: %s\n" "$1" >&2 ;;
        staging_menu) printf "Staging ha fallat: [1] reintentar, [2] continuar sense certificat de confiança, [3] sortir" ;;
        hostd_no_systemd) printf "avís: no hi ha systemd; les actualitzacions des del panell queden desactivades\n" >&2 ;;
        hostd_not_git) printf "avís: %s no és un clon de git; les actualitzacions des del panell queden desactivades\n" "$1" >&2 ;;
        hostd_bad_path) printf "avís: la ruta %s té caràcters que l'ajudant rebutja; les actualitzacions des del panell queden desactivades\n" "$1" >&2 ;;
        hostd_installing) printf "instal·lant l'ajudant d'actualitzacions (talaia-hostd) per al clon a %s\n" "$1" ;;
        self_panel_ok) printf "panell: ok (%s)\n" "$1" ;;
        self_panel_failed) printf "panell: HA FALLAT (%s); consulta: %s\n" "$1" "$2" ;;
        self_agents_ok) printf "agents: ok (%s)\n" "$1" ;;
        self_agents_failed) printf "agents: HA FALLAT (%s); consulta: %s\n" "$1" "$2" ;;
        acme_obtained) printf "Certificat ACME: obtingut\n" ;;
        acme_pending_warning) printf "Certificat ACME: encara pendent; últim avís: %s\n" "$1" ;;
        acme_pending) printf "Certificat ACME: encara pendent (el nucli ho segueix provant; consulta: %s)\n" "$1" ;;
        keep_env) printf "es conserva el .env existent\n" ;;
        no_address) printf "error: no s'ha pogut trobar una adreça per a aquesta màquina; passa --hostname\n" >&2 ;;
        port_busy) printf "error: el port %s ja està en ús; tria'n un altre amb --port\n" "$1" >&2 ;;
        agent_port_busy) printf "error: el port %s ja està en ús; tria'n un altre amb --agent-port\n" "$1" >&2 ;;
        cert_choice_title) printf "Quin certificat ha de fer servir el panell?\n" ;;
        cert_choice_1) printf "  1) automàtic, amb dos registres DNS (recomanat)\n" ;;
        cert_choice_2) printf "  2) amb una clau de l'API d'OVH\n" ;;
        cert_choice_3) printf "  3) cap per ara (CA pròpia de Talaia)\n" ;;
        cert_choice) printf "Opció" ;;
        cert_choice_invalid) printf "error: tria 1, 2 o 3\n" >&2 ;;
        hostname_invalid) printf "error: el nom només pot contenir lletres, dígits, punts, dos punts, guions, comes i claudàtors\n" >&2 ;;
        email_invalid) printf "error: --acme-email no és una adreça vàlida\n" >&2 ;;
        wrote_env) printf "s'ha escrit .env per a %s (panell %s, agents %s)\n" "$1" "$2" "$3" ;;
        env_no_acme) printf "error: packaging/core/.env ja existeix i no activa ACME. Defineix TALAIA_ACME_PROVIDER=ovh i torna a executar (l'instal·lador mai substitueix un .env existent).\n" >&2 ;;
        acme_checked) printf "acme: comprovat\n" ;;
        helper_active) printf "ajudant d'actualitzacions: actiu\n" ;;
        helper_inactive) printf "ajudant d'actualitzacions: no actiu (%s); consulta: sudo systemctl status talaia-update.path\n" "$1" ;;
        helper_no_systemd) printf "ajudant d'actualitzacions: no actiu (systemd no funciona); les actualitzacions des del panell queden desactivades\n" ;;
        helper_not_installed) printf "ajudant d'actualitzacions: no instal·lat (--no-hostd); les actualitzacions des del panell queden desactivades\n" ;;
        building) printf "construint i iniciant talaia-core (la primera compilació triga uns minuts)\n" ;;
        waiting_core) printf "esperant que el nucli respongui\n" ;;
        core_no_answer) printf "error: el nucli no ha respost; consulta: %s\n" "$1" >&2 ;;
        creating_admin) printf "creant l'administrador \"%s\" (la contrasenya necessita almenys 12 caràcters)\n" "$1" ;;
        admin_exists) printf "ja existeix un administrador; no se'n crea cap altre\n" ;;
        continuing_ca) printf "es continua amb la CA de Talaia per ara.\n" ;;
        final_running) printf "talaia-core està en marxa.\n" ;;
        final_panel) printf "  Panell: %s\n" "$1" ;;
        final_agents) printf "  Agents: %s\n" "$1" ;;
        final_cert_ready) printf "  Certificat: de confiança per als navegadors (ACME).\n" ;;
        final_cert_pending) printf "  Certificat: ACME està configurat però encara no està llest; fins llavors, el navegador avisa de la CA de Talaia.\n" ;;
        final_cert_check) printf "  Comprova: %s\n" "$1" ;;
        final_cert_own1) printf "  El navegador avisarà del certificat fins que confiïs en la CA de Talaia:\n" ;;
        final_cert_own2) printf "  baixa %s i instal·la'l com a autoritat de confiança.\n" "$1" ;;
        final_add_server) printf "Per afegir un servidor, crea un token aquí i executa en aquell servidor l'ordre que imprimeix:\n" ;;
        final_update_active) printf "Per actualitzar Talaia, obre Configuració al panell; l'ajudant d'actualitzacions ho aplica.\n" ;;
        final_update_inactive) printf "Per actualitzar Talaia, obre Configuració al panell (l'ajudant d'actualitzacions no està actiu; mira més amunt).\n" ;;
        final_logs) printf "Registres: %s\n" "$1" ;;
        help)
            cat <<'EOF_CA'

Configura talaia-core amb Docker Compose en aquesta màquina: escriu .env amb una
contrasenya aleatòria per a la base de dades, construeix i inicia la pila i crea
l'administrador. Executa'l des d'un clon del repositori; es pot repetir.

Ús:
  packaging/core/install.sh [--hostname <ip-o-nom>] [--port <port>] [--agent-port <port>]
                            [--acme delegation|ovh] [--public-ip <IPv4>] [--acme-zone <zona>]
                            [--acme-email <adreça>] [--no-acme] [--no-acme-check] [--lang <en|es|ca>]
                            [--username <nom>] [--no-user] [--no-hostd] [--install-docker]

  --hostname    nom o adreça des de la qual s'arriba al nucli (es pregunta en un terminal; per defecte: la primera adreça d'aquesta màquina)
  --port        port del panell (per defecte: el primer lliure des de 8443; mai tria 80 ni 443 pel seu compte)
  --agent-port  port dels agents (per defecte: el primer lliure des de 8444)
  --acme        certificat de confiança per al panell: delegation (recomanat; dos registres DNS i
                cap clau de proveïdor) o ovh (una clau de l'API d'OVH). En un terminal
                l'instal·lador pregunta i ofereix la CA pròpia com a tercera opció.
  --public-ip   IPv4 pública per al registre A de la delegació (per defecte: l'origen de la ruta per defecte)
  --acme-zone   zona DNS (el domini registrat; per defecte: les dues últimes etiquetes de --hostname)
  --acme-email  adreça de contacte per a l'autoritat de certificació
  --no-acme     no obtenir un certificat de confiança ara; fer servir la CA de Talaia
  --no-acme-check  no comprovar la clau d'OVH amb l'API d'OVH (instal·lacions sense connexió)
  --lang        idioma dels missatges de l'instal·lador: en, es o ca (per defecte: des de LANG; en)
  --username    nom de l'administrador (per defecte: admin)
  --no-user     no crear l'administrador
  --no-hostd    no instal·lar talaia-hostd, l'ajudant de root que aplica les actualitzacions demanades des del panell
  --install-docker  instal·lar Docker amb l'script oficial si falta (també s'ofereix en un
                terminal; mai s'instal·la en silenci)

Abans d'escriure res comprova el sistema operatiu, git, una manera d'obtenir root, l'espai
lliure i Docker amb el seu connector Compose, i explica com arreglar el que falti. Amb un nom DNS
a --hostname avisa si el nom no resol, i imprimeix les ordres de tallafoc per als dos ports.
Mai obre ports.
EOF_CA
            ;;
        *) return 1 ;;
    esac
}

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
            [ "$#" -ge 2 ] || { msg err_hostname_value; exit 1; }
            hostname_arg="$2"
            shift 2
            ;;
        --port)
            [ "$#" -ge 2 ] || { msg err_port_value; exit 1; }
            port="$2"
            shift 2
            ;;
        --agent-port)
            [ "$#" -ge 2 ] || { msg err_agent_port_value; exit 1; }
            agent_port="$2"
            shift 2
            ;;
        --acme)
            [ "$#" -ge 2 ] || { msg err_acme_value; exit 1; }
            acme_provider="$2"
            shift 2
            ;;
        --acme-email)
            [ "$#" -ge 2 ] || { msg err_acme_email_value; exit 1; }
            acme_email="$2"
            shift 2
            ;;
        --acme-zone)
            [ "$#" -ge 2 ] || { msg err_acme_zone_value; exit 1; }
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
            [ "$#" -ge 2 ] || { msg err_public_ip_value; exit 1; }
            public_ip_arg="$2"
            shift 2
            ;;
        --lang)
            [ "$#" -ge 2 ] || { msg err_lang_value; exit 1; }
            case "$2" in
                en | es | ca) lang="$2" ;;
                *) msg err_lang "$2"; exit 1 ;;
            esac
            shift 2
            ;;
        --username)
            [ "$#" -ge 2 ] || { msg err_username_value; exit 1; }
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
            msg help
            exit 0
            ;;
        *)
            msg unknown_option "$1"
            exit 1
            ;;
    esac
done

for value in "$port" "$agent_port"; do
    case "$value" in
        *[!0-9]*)
            msg ports_numbers
            exit 1
            ;;
    esac
done
if [ -n "$port" ] && [ "$port" = "$agent_port" ]; then
    msg ports_differ
    exit 1
fi
case "$acme_provider" in
    '' | ovh | delegation) ;;
    *)
        msg acme_unsupported "$acme_provider"
        exit 1
        ;;
esac
if [ "$no_acme" -eq 1 ] && [ -n "$acme_provider" ]; then
    msg acme_no_conflict
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
    msg no_free_port "$1"
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
            msg dns_no_getent "$host"
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
                        msg dns_mismatch "$host" "$address" "$local_addresses"
                        ;;
                esac
            fi
        done <<< "$(getent ahosts "$host" 2>/dev/null || true)"
        if [ "$resolved" -eq 0 ]; then
            msg dns_unresolved "$host"
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
            msg fw_note_ufw
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
            msg fw_note_firewalld
        fi
    fi

    if [ "$ufw_active" -eq 1 ]; then
        msg fw_ufw_active
        echo "  sudo ufw allow ${port}/tcp"
        echo "  sudo ufw allow ${agent_port}/tcp"
    elif [ "$firewalld_active" -eq 1 ]; then
        msg fw_firewalld_active
        echo "  sudo firewall-cmd --permanent --add-port=${port}/tcp && sudo firewall-cmd --reload"
        echo "  sudo firewall-cmd --permanent --add-port=${agent_port}/tcp && sudo firewall-cmd --reload"
    else
        msg fw_none
    fi
    msg fw_provider "$port" "$agent_port"
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
            msg pf_os_missing
            ;;
        *)
            msg pf_os_other "$os_id"
            ;;
    esac

    if ! command -v git >/dev/null 2>&1; then
        msg pf_git "sudo apt-get install -y git"
        exit 1
    fi

    if ! command -v curl >/dev/null 2>&1; then
        msg pf_curl "sudo apt-get install -y curl"
        exit 1
    fi

    if [ "$(id -u)" -ne 0 ]; then
        if ! command -v sudo >/dev/null 2>&1; then
            msg pf_root
            exit 1
        fi
        if ! sudo -n true 2>/dev/null; then
            msg pf_sudo_note
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
            msg pf_disk_read "$disk_path"
            ;;
        *)
            if [ "$available_kb" -lt 2097152 ]; then
                msg pf_disk_low "$disk_path"
                exit 1
            fi
            if [ "$available_kb" -lt 5242880 ]; then
                msg pf_disk_warn "$disk_path"
            fi
            ;;
    esac

    if [ -n "$hostname_arg" ]; then
        dns_guidance "$hostname_arg"
    fi

    if ! command -v docker >/dev/null 2>&1; then
        msg pf_docker_missing
        msg pf_docker_official "curl -fsSL https://get.docker.com | sudo sh"
        install_docker_now() {
            if [ "$(id -u)" -eq 0 ]; then
                curl -fsSL https://get.docker.com | sh
            else
                curl -fsSL https://get.docker.com | sudo sh
            fi
        }
        if [ "$install_docker" -eq 1 ]; then
            msg pf_docker_installing
            if ! install_docker_now; then
                msg pf_docker_failed "curl -fsSL https://get.docker.com | sudo sh"
                exit 1
            fi
        elif [ -t 0 ]; then
            if is_yes "$(ask "$(msg ask_docker)" "no")"; then
                msg pf_docker_installing
                if ! install_docker_now; then
                    msg pf_docker_failed "curl -fsSL https://get.docker.com | sudo sh"
                    exit 1
                fi
            else
                msg pf_docker_required "curl -fsSL https://get.docker.com | sudo sh"
                exit 1
            fi
        else
            msg pf_docker_required "curl -fsSL https://get.docker.com | sudo sh"
            exit 1
        fi
        if ! command -v docker >/dev/null 2>&1; then
            msg pf_docker_still "curl -fsSL https://get.docker.com | sudo sh"
            exit 1
        fi
        if ! docker info >/dev/null 2>&1; then
            msg pf_docker_sudo
        fi
    fi

    if ! docker compose version >/dev/null 2>&1; then
        msg pf_compose "sudo apt-get install -y docker-compose-plugin" "docker-compose-v2"
        exit 1
    fi

    if [ "${TALAIA_INSTALL_PREFLIGHT_ONLY:-0}" = "1" ]; then
        msg pf_ok
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
            zone="$(ask "$(msg zone_prompt)" "$zone")"
        fi
    fi
    if [ -z "$zone" ]; then
        msg zone_missing
        exit 1
    fi
    case "$zone" in
        *[!A-Za-z0-9.-]* | .* | *. | *..*)
            msg zone_invalid
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
    msg wrote_acme
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
        msg cleanup_staging
        msg finish_later "cd $SCRIPT_DIR && ./install.sh --acme delegation" >&2
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
        msg ovh_sha1
        exit 1
    fi

    if ! time_response="$(curl -sS --max-time 15 -w '\n%{http_code}' "$base/auth/time" 2>"$acme_tmp/curl.err")"; then
        msg ovh_unreachable "$base"
        return 2
    fi
    time_code="${time_response##*$'\n'}"
    time_body="${time_response%$'\n'*}"
    if [ "$time_code" != "200" ]; then
        msg ovh_unreachable_http "$base" "$time_code"
        return 2
    fi
    timestamp="$(printf '%s' "$time_body" | tr -d '[:space:]')"
    case "$timestamp" in
        '' | *[!0-9]*)
            msg ovh_invalid_time
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
        msg ovh_unreachable "$base"
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
                    msg ovh_key_ok
                    return 0
                fi
                for right in "${missing[@]}"; do
                    msg ovh_missing_right "$right"
                done
                return 1
                ;;
            '')
                message="$(printf '%s' "$body" | tr -d '\r\n' | sed -n 's/.*"message"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
                [ -n "$message" ] || message="$body"
                safe="$(sanitize_ovh_text "$message")"
                msg ovh_rejected "$safe"
                return 1
                ;;
            *)
                msg ovh_not_validated
                return 1
                ;;
        esac
    fi

    message="$(printf '%s' "$body" | tr -d '\r\n' | sed -n 's/.*"message"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')"
    [ -n "$message" ] || message="$body"
    safe="$(sanitize_ovh_text "$message")"
    msg ovh_rejected_http "$code" "$safe"
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
            msg acme_file_incomplete
            exit 1
        fi
        case "$acme_key$acme_secret$acme_consumer" in
            *[!A-Za-z0-9._-]*)
                msg acme_file_chars
                exit 1
                ;;
        esac
    fi
    if [ -z "$endpoint" ]; then
        endpoint="${OVH_ENDPOINT:-}"
    fi
    if [ -z "$endpoint" ] && [ "$existing" -eq 0 ] && [ -t 0 ]; then
        endpoint="$(ask "$(msg ovh_region_prompt)" "ovh-eu")"
    fi
    : "${endpoint:=ovh-eu}"
    case "$endpoint" in
        ovh-eu | ovh-ca) ;;
        *)
            msg ovh_region_invalid "$endpoint"
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
    msg ovh_create_key "$zone"
    echo "  $create_link"
    msg ovh_guide "$guide"

    if [ "$existing" -eq 0 ]; then
        key="${OVH_APPLICATION_KEY:-}"
        secret="${OVH_APPLICATION_SECRET:-}"
        consumer="${OVH_CONSUMER_KEY:-}"
    fi

    if [ -t 0 ] && [ "$existing" -eq 0 ] && { [ -z "$key" ] || [ -z "$secret" ] || [ -z "$consumer" ]; }; then
        read -r -p "$(msg ovh_enter_values)" answer || true
    fi

    while :; do
        if [ "$existing" -eq 0 ]; then
            if [ -t 0 ]; then
                [ -n "$key" ] || read -r -p "$(msg ovh_app_key)" key
                if [ -z "$secret" ]; then
                    read -r -s -p "$(msg ovh_secret)" secret
                    echo
                fi
                if [ -z "$consumer" ]; then
                    read -r -s -p "$(msg ovh_consumer)" consumer
                    echo
                fi
            fi
            if [ -z "$key" ] || [ -z "$secret" ] || [ -z "$consumer" ]; then
                msg ovh_missing_env
                exit 1
            fi
            case "$key$secret$consumer" in
                *[!A-Za-z0-9._-]*)
                    msg ovh_bad_chars
                    exit 1
                    ;;
            esac
            acme_key="$key"
            acme_secret="$secret"
            acme_consumer="$consumer"
            acme_endpoint="$endpoint"
        fi

        if [ "$no_acme_check" -eq 1 ]; then
            msg ovh_skip
            break
        fi

        result=0
        check_ovh_key "$endpoint" "$zone" || result=$?
        if [ "$result" -eq 0 ]; then
            break
        fi

        if [ "$result" -eq 2 ] && [ -t 0 ]; then
            if is_yes "$(ask "$(msg ovh_reach_prompt)" "no")"; then
                msg ovh_continue_unchecked
                break
            fi
        fi
        if [ ! -t 0 ]; then
            msg ovh_validate_failed
            exit 1
        fi
        answer="$(ask "$(msg ovh_menu)" "1")"
        case "$answer" in
            1)
                msg ovh_enter_again
                key=""
                secret=""
                consumer=""
                existing=0
                ;;
            2)
                msg ovh_off
                if grep -q '^TALAIA_ACME_PROVIDER=' .env 2>/dev/null; then
                    sed -i 's|^TALAIA_ACME_PROVIDER=.*|TALAIA_ACME_PROVIDER=|' .env
                else
                    printf 'TALAIA_ACME_PROVIDER=\n' >> .env
                fi
                msg ovh_turned_off
                TALAIA_ACME_PROVIDER=""
                acme_state=none
                return 0
                ;;
            3 | *)
                msg ovh_abort
                exit 1
                ;;
        esac
    done

    if [ "$existing" -eq 1 ]; then
        msg keep_acme
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
            ip="$(ask "$(msg ip_prompt)" "")"
        else
            msg ip_not_found
            exit 1
        fi
    fi
    while :; do
        if ! is_ipv4 "$ip"; then
            msg ip_invalid "$ip"
            if [ -t 0 ]; then
                ip="$(ask "$(msg ip_prompt)" "")"
                continue
            fi
            exit 1
        fi
        if ! is_private_ipv4 "$ip"; then
            break
        fi
        msg ip_nat "$ip"
        if [ -t 0 ]; then
            ip="$(ask "$(msg ip_prompt)" "")"
            continue
        fi
        msg ip_nat_fix
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
        msg ss_missing
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
        msg port53_own
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
    msg port53_conflict "$conflict" "${process:+ ($process)}"
    msg port53_need "$delegation_public_ip"
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
        '' | *[!A-Za-z0-9_]* | [0-9]*) msg env_key_invalid "$key"; return 1 ;;
    esac
    if ! validate_env_value "$value"; then
        msg env_value_invalid "$key"
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
    msg acme_off
    msg finish_later "cd $SCRIPT_DIR && ./install.sh --acme delegation"
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
            msg rec_outside "$host" "$zone"
            prefix="$host"
        fi
        a_name="$prefix"
        ns_name="_acme-challenge.$prefix"
    fi
    echo
    msg rec_create "$zone"
    echo
    printf '%-5s %-25s %s\n' "$(msg rec_type)" "$(msg rec_name)" "$(msg rec_value)"
    printf '%-5s %-25s %s\n' "A" "$a_name" "$delegation_public_ip"
    printf '%-5s %-25s %s\n' "NS" "$ns_name" "${host}."
    echo
    msg rec_no_aaaa "$host"
    msg rec_guide "https://github.com/catlinux/Talaia/blob/main/docs/en/dns-delegation.md"
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
            msg dfw_note_ufw
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
            msg dfw_note_firewalld
            echo "  sudo firewall-cmd --permanent --add-port=${port}/tcp --add-port=${agent_port}/tcp --add-port=53/udp --add-port=53/tcp && sudo firewall-cmd --reload"
        fi
    fi

    msg dfw_ports
    msg dfw_ports_detail "$port" "$agent_port"
    if [ "$ufw_active" -eq 1 ]; then
        if [ -t 0 ] && [ "$can_root" -eq 1 ] && is_yes "$(ask "$(msg dfw_ufw_question)" "no")"; then
            "${as_root[@]}" ufw allow "${port}/tcp"
            "${as_root[@]}" ufw allow "${agent_port}/tcp"
            "${as_root[@]}" ufw allow 53/udp
            "${as_root[@]}" ufw allow 53/tcp
            msg dfw_opened_ufw
        else
            msg dfw_ufw_active
            echo "  sudo ufw allow ${port}/tcp"
            echo "  sudo ufw allow ${agent_port}/tcp"
            echo "  sudo ufw allow 53/udp"
            echo "  sudo ufw allow 53/tcp"
        fi
    elif [ "$firewalld_active" -eq 1 ]; then
        if [ -t 0 ] && [ "$can_root" -eq 1 ] && is_yes "$(ask "$(msg dfw_firewalld_question)" "no")"; then
            "${as_root[@]}" firewall-cmd --permanent --add-port="${port}/tcp"
            "${as_root[@]}" firewall-cmd --permanent --add-port="${agent_port}/tcp"
            "${as_root[@]}" firewall-cmd --permanent --add-port=53/udp
            "${as_root[@]}" firewall-cmd --permanent --add-port=53/tcp
            "${as_root[@]}" firewall-cmd --reload
            msg dfw_opened_firewalld
        else
            msg dfw_firewalld_active
            echo "  sudo firewall-cmd --permanent --add-port=${port}/tcp --add-port=${agent_port}/tcp --add-port=53/udp --add-port=53/tcp && sudo firewall-cmd --reload"
        fi
    else
        if command -v ufw >/dev/null 2>&1 || command -v firewall-cmd >/dev/null 2>&1; then
            msg dfw_installed_inactive
        else
            msg dfw_none
        fi
    fi
    msg dfw_provider "$port" "$agent_port"
    msg dfw_never
    if [ -t 0 ]; then
        read -r -p "$(msg dfw_enter)" _ || true
    fi
    delegation_firewall_done=1
}

# setup_delegation prepares the acme-dns delegation before the build: public IP,
# port 53, .env keys, records and firewall. It may switch to OVH or ACME off.
setup_delegation() {
    local host choice=""
    if ! is_dns_name "${TALAIA_PUBLIC_HOSTNAMES%%,*}"; then
        msg del_needs_dns
        msg del_use_alt
        exit 1
    fi
    host="${TALAIA_PUBLIC_HOSTNAMES%%,*}"
    delegation_host="${host%.}"
    detect_public_ipv4
    if ! check_port_53; then
        if [ -t 0 ]; then
            msg del_port53_in_use
            msg del_choice_ovh
            msg del_choice_off
            msg del_choice_quit
            choice="$(ask "$(msg del_choice)" "2")"
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
        msg del_stop
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
        msg check_attempt "$attempt" "$max"
        if "${compose[@]}" run --rm --no-deps core acme check-delegation --hostname "$delegation_host" --address "$delegation_public_ip"; then
            return 0
        fi
        if [ -t 0 ]; then
            msg check_wait
            choice="$(ask "$(msg check_menu)" "")"
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
                *) msg check_unknown ;;
            esac
        else
            if [ "$attempt" -ge "$max" ]; then
                msg check_gave_up "$max"
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
    msg staging_restart
    "${compose[@]}" restart core
    restart_at="$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || true)"
    for i in $(seq 1 48); do
        log="$("${compose[@]}" logs --since "$restart_at" core 2>/dev/null || true)"
        if printf '%s\n' "$log" | grep -qE 'ACME certificate ready|using the stored ACME certificate'; then
            msg staging_ok
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
    msg staging_failed
    if [ -n "$warning" ]; then
        msg staging_warning "$warning"
    fi
    if [ -t 0 ]; then
        choice="$(ask "$(msg staging_menu)" "1")"
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
        msg hostd_no_systemd
        return 0
    fi
    if [ ! -d "$repo_dir/.git" ]; then
        msg hostd_not_git "$repo_dir"
        return 0
    fi
    if [[ ! "$repo_dir" =~ ^/[A-Za-z0-9._/-]+$ ]]; then
        msg hostd_bad_path "$repo_dir"
        return 0
    fi
    repo_user="$(stat -c %U "$repo_dir")"
    [ "$(id -u)" -eq 0 ] || as_root=(sudo)

    msg hostd_installing "$repo_dir"
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
        msg self_panel_ok "https://127.0.0.1:${port}/healthz"
    else
        msg self_panel_failed "https://127.0.0.1:${port}/healthz" "${compose[*]} logs core"
    fi
    if curl -fsk -o /dev/null "https://127.0.0.1:${agent_port}/ca.pem" 2>/dev/null; then
        msg self_agents_ok "https://127.0.0.1:${agent_port}/ca.pem"
    else
        msg self_agents_failed "https://127.0.0.1:${agent_port}/ca.pem" "${compose[*]} logs core"
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
            msg acme_obtained
            return 0
        fi
        if [ -z "$since" ]; then
            log="$("${compose[@]}" logs --since "$run_started" core 2>/dev/null || true)"
        fi
        warning="$(printf '%s\n' "$log" | grep -Ei 'acme|renewal failed' | grep -E '"level":"(WARN|ERROR)"|renewal failed' | tail -n 1 || true)"
        sleep 5
    done
    if [ -n "$warning" ]; then
        msg acme_pending_warning "$warning"
    else
        msg acme_pending "${compose[*]} logs core | grep ACME"
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
    msg keep_env
else
    if [ -z "$hostname_arg" ]; then
        hostname_arg="$(hostname -I 2>/dev/null | cut -d' ' -f1 || true)"
        if [ -t 0 ]; then
            hostname_arg="$(ask "$(msg hostname_prompt)" "$hostname_arg")"
        fi
    fi
    if [ -z "$hostname_arg" ]; then
        msg no_address
        exit 1
    fi

    # A port given on the command line must be free; otherwise the first free one is used.
    if [ -n "$port" ] && port_in_use "$port"; then
        msg port_busy "$port"
        exit 1
    fi
    if [ -n "$agent_port" ] && port_in_use "$agent_port"; then
        msg agent_port_busy "$agent_port"
        exit 1
    fi
    [ -n "$port" ] || port="$(pick_port 8443 "$agent_port")"
    [ -n "$agent_port" ] || agent_port="$(pick_port 8444 "$port")"

    if [ -z "$acme_provider" ] && [ "$no_acme" -eq 0 ] && [ -t 0 ] && is_dns_name "$hostname_arg"; then
        msg cert_choice_title
        msg cert_choice_1
        msg cert_choice_2
        msg cert_choice_3
        while :; do
            acme_choice="$(ask "$(msg cert_choice)" "1")"
            case "$acme_choice" in
                1) acme_provider="delegation"; break ;;
                2) acme_provider="ovh"; break ;;
                3) acme_provider=""; break ;;
                *) msg cert_choice_invalid ;;
            esac
        done
    fi
    if [ -n "$acme_provider" ]; then
        if [ -z "$acme_email" ] && [ -t 0 ]; then
            acme_email="$(ask "$(msg email_prompt)")"
        fi
    fi

    # Both go into .env unquoted and through sed: only plain characters.
    hostname_pattern='^[][A-Za-z0-9.:,-]+$'
    if [[ ! "$hostname_arg" =~ $hostname_pattern ]]; then
        msg hostname_invalid
        exit 1
    fi
    email_pattern='^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+$'
    if [ -n "$acme_email" ] && [[ ! "$acme_email" =~ $email_pattern ]]; then
        msg email_invalid
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
    msg wrote_env "$hostname_arg" "$port" "$agent_port"
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
    msg env_no_acme
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
    msg acme_checked
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
            msg helper_active
        else
            msg helper_inactive "${helper_state:-unknown}"
        fi
    else
        msg helper_no_systemd
    fi
else
    msg helper_not_installed
fi

run_started="$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || true)"

msg building
"${compose[@]}" up -d --build

msg waiting_core
ready=0
for _ in $(seq 1 60); do
    if curl -fsk -o /dev/null "https://127.0.0.1:${port}/healthz" 2>/dev/null; then
        ready=1
        break
    fi
    sleep 2
done
if [ "$ready" -ne 1 ]; then
    msg core_no_answer "${compose[*]} logs core"
    exit 1
fi

self_check

if [ "$create_user" -eq 1 ]; then
    users="$("${compose[@]}" exec -T db psql -U talaia -d talaia -tAc 'SELECT count(*) FROM app_user' 2>/dev/null | tr -d '[:space:]' || true)"
    if [ "$users" = "0" ]; then
        msg creating_admin "$username"
        "${compose[@]}" run --rm core user create --username "$username"
    else
        msg admin_exists
    fi
fi

if [ "$delegation_active" -eq 1 ]; then
    if delegation_check; then
        # A failed staging turns ACME off and returns 1; the summary below must
        # still be printed, so set -e must not stop here.
        delegation_staging || true
    else
        msg continuing_ca
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
msg final_running
echo
msg final_panel "$panel_url"
msg final_agents "$agent_url"
case "$acme_state" in
    ready)
        msg final_cert_ready
        ;;
    pending)
        msg final_cert_pending
        msg final_cert_check "cd $SCRIPT_DIR && ${compose[*]} logs core | grep ACME"
        ;;
    *)
        msg final_cert_own1
        msg final_cert_own2 "${agent_url}/ca.pem"
        ;;
esac
echo
msg final_add_server
echo
echo "  cd $SCRIPT_DIR && ${compose[*]} run --rm core token create --name <server-name>"
echo
if [ "$install_helper" -eq 1 ] && [ "$helper_active" -eq 1 ]; then
    msg final_update_active
else
    msg final_update_inactive
fi
msg final_logs "cd $SCRIPT_DIR && ${compose[*]} logs -f core"
