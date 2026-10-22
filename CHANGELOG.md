# Changelog
- Terminal sessions in the agent (not reachable from the panel yet): a shell from a closed list (`/bin/sh` or `/bin/bash`) runs through the Docker exec API with a terminal, as the user of the container and without privileges, and ends with the stream to the core. Limits: 4 sessions, 15 minutes without activity, 4 hours in total, 16 KiB per output message and 1 MiB/s of output; a shell that does not read its input is closed instead of stalling the agent. It reports whether the container is as good as root on the host (privileged, host PID namespace or Docker socket mounted).
- Terminal sessions in the core (not reachable from the panel yet): sessions are routed by server and session id, so an agent can only feed its own; at most 4 per server and one per user and container; every session is recorded as an asciicast v2 file in chunks sealed with the secrets key and bound to their position, so a dropped, repeated or swapped chunk is detected on export; a session that cannot be recorded (database down, 50 MiB reached) is closed; keystrokes are not recorded. Keystrokes wait for a busy agent instead of being lost, and a browser that reads too slowly ends its session. Audited as `terminal_open` and `terminal_close`.
- `talaia-core terminal list` and `talaia-core terminal export <id>` (prints the recording for `asciinema play`), and the setting `terminal_recording_days` / `TALAIA_TERMINAL_RECORDING_DAYS` (default 90, 0 keeps them for ever) with a daily purge. Sessions a restart left open are closed at start.
- Terminal API: `POST /api/v1/servers/{id}/containers/{c}/terminal` (full session; needs `confirm`, a shell of `sh` or `bash`) starts the shell and answers with a one-time ticket, and `GET /api/v1/terminal/{id}/ws` is the WebSocket (binary messages are keystrokes and output, text messages are control: ticket, resize, closed). The ticket lives 30 seconds, is stored as a hash, works once, and only for the session it was issued for from the panel session that asked for it; a shell nobody attaches to is closed. The WebSocket requires an allowed `Origin` (there is no same-site shortcut) and the session cookie, and the panel session is checked every minute: revoking it closes the terminal. The core closes its sessions on shutdown.
- Terminal in the panel: a **Terminal** button on every running container opens a shell with xterm.js (served from the panel itself and loaded only when used). It asks first, explaining that the session is recorded and that what you type is kept when the screen shows it, and lets you choose `sh` or `bash`; it shows a permanent *Recording* mark, a red warning when the container is as good as root on the server (privileged, host processes or the Docker socket), and why a session ended (exit code, inactivity, revoked session…) with a button to open a new one. In ca/es/en, for light and dark themes and small screens.

[Leer en castellano](CHANGELOG.es.md)

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). The current version is in the
[`VERSION`](VERSION) file.

Sections used in each release: **Added**, **Changed**, **Deprecated**, **Removed**, **Fixed**,
**Security**.

## [Unreleased]

## [0.3.5] - 2026-10-22

### Added

- Updates in the panel (read only): the server screen has an **Updates** section with the pending packages (security first), held packages, why a reboot is needed, when the lists were last updated and a **Check now** button that runs `apt-get update`; fleet cards show an updates badge (red when there are security updates) and a **Reboot required** badge. In ca, es and en.

## [0.3.4] - 2026-10-22

### Added

- The core keeps the apt summary and the host terminal flag of each connected agent and shows them in `GET /api/v1/servers` (`live.apt`, `live.host_terminal`); `POST /api/v1/servers/{id}/apt/status` asks the agent for the full list of pending updates, held packages and reboot reasons (with `refresh` it runs `apt-get update` first). Not shown in the panel yet.

## [0.3.3] - 2026-10-22

### Added

- The agent reads apt without changing anything: it simulates an upgrade at start and every 6 hours, counts pending and security updates, held packages and whether a reboot is required (flag file or a newer installed kernel), sends the summary in its inventory and answers the `AptStatus` command with the full list (optionally running `apt-get update` first). Not shown in the panel yet.

## [0.3.2] - 2026-10-22

### Added

- Agent contract for phase 3 (whole server): typed commands for systemd services, journald, apt status and upgrades, and reboot; an apt summary and the host terminal flag in the inventory; the enabled state of each service; a target for terminal sessions (container or host). Nothing uses them yet.

## [0.3.1] - 2026-10-22

### Changed

- **License changed to WNCL-TP-1.0.** It covers only Talaia's original material and states that third-party components keep their own licenses. The terms for Talaia itself are the same: free non-commercial use, study, modification and sharing; selling or commercial use needs written permission. The About screen and the agent package show the new identifier.

### Added

- `CREDITS.md` and `CREDITS.es.md` list every third-party component in the binaries and the web panel (40 Go modules, 40 npm packages and 3 OFL fonts) with version, license and link.
- `THIRD-PARTY-NOTICES.txt` with the license text and copyright notice of each component, generated by `scripts/third-party-notices.mjs`. It ships in the agent package (`/usr/share/doc/talaia-agent/`) and in the core image (`/usr/share/doc/talaia/`), together with `LICENSE`, and CI fails if it is out of date.

## [0.3.0] - 2026-10-22

### Changed

- **Phase 2 (containers and stacks) closed.** Containers, stacks with the Compose editor, adoption of running projects, images/volumes/networks with cleanup, live logs, the stack screen and the recorded container terminal were tested on two real servers; the security review of the phase is done. The roadmap marks phases 0 to 2 as done and phase 3 (whole server) comes next.

### Security

- PIN sign-in: an attempt is now counted before the PIN is checked, in one statement with its limit, so requests sent at the same time cannot test more than five PINs per trusted device (before, parallel requests from several addresses could all be checked before the counter disabled the device). Found in the security review that closes phase 2, which also covered passkeys, agent updates and the container terminal without other findings.

## [0.2.15] - 2026-10-22

### Fixed

- An adopted stack whose directory or compose file disappears from the server no longer stays as a dead entry: the agent reports it (`broken_stacks`, proto field 16), `GET /api/v1/servers/{id}/stacks` lists it under `broken`, and the panel marks it **Files missing** and offers only **Stop managing** (the other actions answer that the files are gone). Older agents keep working without the mark.

## [0.2.14] - 2026-10-22

### Changed

- Access guide: it now says that "browser closed" means the browser discarded its session cookies, so Chrome on a computer with *Continue where you left off* or background apps keeps the PIN without the second factor until `device_mfa_hours` passes, and how to change it.

## [0.2.13] - 2026-10-22

### Fixed

- Container terminal: on some browsers the terminal grew without end and then stopped taking keys, with thousands of "WebSocket is already in CLOSING or CLOSED state" errors in the console. The terminal box now has a height of its own (it no longer follows its content), a resize is sent only when the number of rows or columns really changes and never through a closed connection, and the size is no longer watched once the session ended.

## [0.2.12] - 2026-10-22

### Changed

- Stopping a container from the server screen now asks for confirmation first, like removing it (start and restart stay immediate), since stopping a database or a web server by mistake takes services down.

## [0.2.11] - 2026-10-22

### Changed

- Adopting a Compose project whose directory or file others can write now fails with the exact command to fix it (`chmod go-w '<directory>' '<file>'`) instead of a generic sentence.

## [0.2.10] - 2026-10-22

### Fixed

- Panel: the confirmation dialogs (update, remove a server, update an agent) opened at the bottom of long pages, because the entrance animation of the page kept it as the reference of fixed elements; they now open centred on the window. In Settings, the update progress card now appears right under the version card instead of at the end of the page. The panel tests wait up to five seconds for an element, so a busy machine no longer fails them at random.

## [0.2.9] - 2026-10-22

### Fixed

- Agents older than 0.2.8 ignore the update order, so the panel left the update "in progress" for ever. The core now marks them `agent_update_manual`, sends the upgrade command in `agent_upgrade_command`, and the panel shows a **Manual update** badge with the command to copy instead of an Update button; once on 0.2.8 they update from the panel. A stuck update job stops blocking new attempts after 15 minutes.

## [0.2.8] - 2026-10-21

### Added

- Update the agents from the panel: the server cards and screens show the agent version and an **Update available** badge, the server screen has an **Update agent** button and **Settings > Servers** has **Update all agents**. The core sends a new typed order (`AgentUpdate`, proto field 22) with the version and the SHA-256 of the package it serves; the agent downloads it over its mutual-TLS channel (64 MiB limit), refuses an equal or older version or a wrong hash, and installs it with `apt-get` in a detached systemd unit, so the restart of the agent does not interrupt it. New API: `POST /api/v1/servers/{id}/agent/update`, `POST /api/v1/agents/update-all` and the `agent_update_available` field of `GET /api/v1/servers`. Audited as `agent_update`. Security guide updated.

## [0.2.7] - 2026-10-21

### Fixed

- Asking for an enrollment command for a server name that still had an unused token (up to 15 minutes) failed with "name taken". The new token now replaces the pending one, so a lost command can be asked for again at once.

## [0.2.6] - 2026-10-21

### Fixed

- Re-installing an agent after removing its server from the panel kept the revoked identity (the core answered 403 for ever) and ignored the new token. An enrollment token left next to a stored identity now makes the agent enroll once with it, whatever core issued the old identity; if that fails it keeps the stored identity. This replaces the earlier check that only compared the CA fingerprint.

## [0.2.5] - 2026-10-21

### Added

- Access guide (`docs/en/access.md`, also in Spanish and Catalan): PIN on a trusted device, passkeys, the second-factor window and how to undo each method. The installation guide now explains why the panel address carries a port (8443 unless `--port 443` is free), how an agent enrolls again and how to remove a server; the README and the security guide link to the new guide.

## [0.2.4] - 2026-10-21

### Added

- Sign in with a PIN or a fingerprint in the panel: the login screen offers the PIN of a trusted device first (with a link to the password form and the existing TOTP step when the second factor is due) and a **fingerprint or PIN of this device** button for passkeys. Settings has a new **Access** card to trust or forget devices (PIN twice and the password) and to add or remove passkeys, with a notice when passkeys are not available. In ca, es and en.

## [0.2.3] - 2026-10-21

### Added

- Passkeys (WebAuthn, fingerprint or the PIN of the device) in the core API; panel screens follow. They need a DNS name in `public_hostnames` and HTTPS (otherwise `passkeys_unavailable`). A passkey always requires user verification and counts as password plus second factor. Registration needs a full session and the password again (up to 10 per user); `POST /api/v1/auth/passkey/login/begin` and `/finish` sign in; `GET` and `DELETE /api/v1/auth/passkeys` list and remove them. Challenges live 5 minutes and work once; a signature counter that goes back disables the passkey (`passkey_clone_suspected`). New migration `0010`.

## [0.2.2] - 2026-10-21

### Added

- Trusted devices with a PIN (core API, panel screens follow): from a full session and with the password confirmed again, a browser can be registered as a trusted device with a PIN of 6 to 12 digits (no repeated digits or straight runs). `POST /api/v1/auth/pin/login` then signs in with the PIN plus the device cookie. The second factor is asked again when the browser was closed or after `device_mfa_hours` (`TALAIA_DEVICE_MFA_HOURS`, default 12, 1 to 168). Five wrong PINs disable the device; unknown device, wrong PIN and disabled device answer the same. At most 10 devices per user; `GET /api/v1/auth/devices` and `DELETE /api/v1/auth/devices/{id}` list and remove them, and changing the password removes them all. Audited as `device_trusted`, `login_pin`, `login_pin_failed` and `device_removed`. New migration `0009`.

## [0.2.1] - 2026-10-21

### Added

- Settings: a **Servers** card lists the enrolled servers and lets the administrator remove one (`DELETE /api/v1/servers/{id}`, full session and `confirm`; the panel asks to type the server name). The removal is soft: the server leaves the panel, its live connection is closed, its certificate is revoked and its name is free to enroll again; terminal recordings are kept. Audited as `server_removed`. New migration `0008`.
- Updates say what the new version brings: Settings shows a **What's new** list with the CHANGELOG entries added between the running build and the offered one, in the language of the panel (Catalan uses the Spanish text), above the commit list. The core reads both changelogs from `raw.githubusercontent.com` at the two commits; if that fails the card shows only the commits, as before.
- The core installer and the `install-core.sh` bootstrap speak English, Spanish and Catalan: every question, error, note and the help text come from a message catalog. The language is taken from `--lang <en|es|ca>` or from `LC_ALL`/`LC_MESSAGES`/`LANG`, with English as the default; the answers stay `yes`/`no` (`sí` and `s` are also accepted in Spanish and Catalan). `test-install.sh` covers the selection and a translated error in each language.
- Guide for the trusted certificate with two DNS records (`docs/en/dns-delegation.md`, also in Spanish and Catalan): the records to create, with the OVH steps, port 53, what each line of the check means, security and how to undo it. The installation guide and the README now recommend it, with the OVH key as the alternative.
- Trusted certificate without DNS provider keys: with `acme_provider = delegation` the zone delegates `_acme-challenge.<host>` to the panel server with an NS record, and the new `acme-dns` service (`talaia-core dns`, compose profile `acme-dns`) answers the DNS-01 challenge on port 53. It runs apart from the core, read only, without the database or the secrets, answers only that name, never recurses and limits each source network. `talaia-core acme check-delegation` tells which record is missing or wrong and whether port 53 answers from the internet.
- OVH guide for the trusted certificate (`docs/en/ovh-dns.md`, also in Spanish and Catalan): how to create the API key allowed to change only your DNS zone, with the exact rights, what to copy and what to do when something fails; linked from the installation guide.
- Security guide (`docs/en/security.md`): the administrator is root on every managed server (Compose files, adoption, the container terminal, updates), what protects the panel and the agents, and how terminal sessions are recorded, kept and exported (`terminal list`, `terminal export`).
- New `install-core.sh` bootstrap: the one-line `curl … | bash` clones the current `main` into `/opt/talaia` (`TALAIA_DIR`, `TALAIA_REPO_URL`, `TALAIA_BRANCH`), reuses an existing clone of the same repository without pulling, asks before installing git, and passes every installer flag through `bash -s --`; stdin reconnects to `/dev/tty` when it runs through a pipe. The ACME/OVH step now asks for the zone and region, prints the prepared key link and guide, checks the key with OVH (the signature travels in a mode 0600 config file, never on the command line) before writing `.env.acme` or building, names missing rights, offers to re-enter the keys or continue with the own CA, validates an existing `.env.acme`, and adds `--acme-zone` and `--no-acme-check`. README and installation docs updated; `test-install.sh` covers the signature, the failure cases, the zone guess and the bootstrap.
- Settings: a **Check now** button asks GitHub for new commits at once through `POST /api/v1/system/check` (one forced check per minute; `429 rate_limited` shows a short notice) instead of waiting for the 30 minute check; the panel replaces the version card with the answer.
- Certificate without a provider key: the installer offers DNS delegation as the recommended choice, detects the public IPv4 and checks port 53, prints the two DNS records and the AAAA warning, asks before opening the panel/agent TCP ports and UDP/TCP 53 in ufw or firewalld, verifies with `talaia-core acme check-delegation`, obtains a staging certificate, switches to production and removes staging on failure; adds `--acme delegation`, `--public-ip` and `--no-acme`. `test-install.sh` covers the IP and port checks, the `.env` keys, the records, the check loop and the staging switch.
- Installation on a fresh VPS: the installer checks the OS, git, root or sudo, disk space and Docker with the Compose plugin before writing anything and names the fix; `--install-docker` (or a yes in a terminal) installs Docker with the official script; it warns about a DNS name that does not resolve, prints the ufw or firewalld commands for the two ports (never opening them), verifies the panel and agent endpoints, waits up to three minutes for the ACME certificate, reports the update helper and ends with the URLs, how to add a server, how to update from Settings and where the logs are. `test-install.sh` covers the preflight in CI.
- The stack screen (mockup 3): click a stack name to see its breadcrumb, the running count, the actions (deploy, restart, stop with confirmation and pull), a table of its containers with CPU and memory, an always-visible note in the compose editor explaining that a Compose file runs as root on the server, and the container logs with a **Follow** mode that asks every 3 seconds for the lines after the last one, keeps them deduplicated and caps them at 2000 lines.
- Container logs can be requested incrementally: the logs job accepts an optional RFC 3339 `since` and a `timestamps` flag end to end, so the panel can poll only the new lines; finished log jobs are kept for 15 minutes and then purged so the job table does not grow.
- Update helper: it installs its own new version when an update changes it, refuses requests older than 30 minutes, and the panel reports a request nobody took for five minutes as a helper that is not running instead of waiting for ever.
- Updates from the panel: `install.sh` installs `talaia-hostd`, a root helper started by `talaia-update.path` when the core drops a request in `/var/lib/talaia-host/requests/`. It accepts only a commit on `origin/main` newer than the running one, backs up the database (last three kept in `/var/lib/talaia-hostd/backups`), fast-forwards the clone as its owner, rebuilds, waits for `/healthz` and, if the new core does not answer, restores the previous commit and the backup. The core never gets the Docker socket; `--no-hostd` skips the helper. The image records its commit (`TALAIA_GIT_COMMIT`).
- Installation guide: running the core on a public server without ports 80/443, the trusted certificate by ACME DNS-01 with OVH (minimum API rights) and the update check in Settings; the VPN stays as an alternative.
- Settings screen in the panel: the running version and commit, the pending commits of the newest green build with their CI badge, and the update button with a confirmation that explains that the panel will be unavailable for a few minutes and that the database is backed up first. The screen follows the host helper's progress every three seconds, survives a restart of the core during the update and resumes after a page reload, and shows the log when an update fails or is rolled back.
- Update detection and the update request API: `GET /api/v1/system/version` reports the running version and commit, the newest commit of `main` whose CI is green, the commits pending since the running build and the state the host helper writes; `POST /api/v1/system/update` (full session, confirmation and only the offered commit) queues `requests/update.json` for the helper and is audited as `system_update_requested`. The core asks the public GitHub API every 30 minutes (`update_repo`, `update_check_interval`, `update_api_url`), reuses ETags, backs off when the API rate limit runs out and keeps working if GitHub is unreachable. The build stamps the commit with `-X main.commit` (the `GIT_COMMIT` build argument) and `host_dir` is the directory shared with the helper.
- The panel can serve a certificate that browsers already trust: when `acme_provider` / `TALAIA_ACME_PROVIDER` is set (currently `ovh`), the core obtains it over ACME DNS-01 with lego, without a reverse proxy and without using ports 80/443, stores the account and the certificate in `<data_dir>/acme/`, renews it 30 days before it expires with backoff on failures and swaps it without a restart, and keeps serving the certificate of Talaia's own CA until the public one is ready; `acme_email` and `acme_directory_url` (for staging or a test server) are the other settings.
- Contract for terminals in containers (`TerminalOpen`, `TerminalInput`, `TerminalResize`, `TerminalClose` from the core; `TerminalOpened`, `TerminalOutput`, `TerminalClosed` from the agent). Not used yet: it only defines the messages.
- Monorepo skeleton: Go workspace, empty `core`, `agent` and `cli` modules, `Makefile` and `scripts/bump-version.sh`.
- Agent contract `proto/agent.proto` v0 (`Heartbeat`, `Inventory`, `Metrics`) with `buf` configuration.
- Core skeleton: HTTP server with `/healthz`, TOML + environment configuration, PostgreSQL connection and first migration.
- Web panel skeleton: Vite + React + TypeScript, design tokens with light/dark theme, i18next (ca/es/en) with language selector and About screen.
- QR code on the second-factor setup screen, drawn as inline SVG so it works under the panel's CSP.
- The core serves `install.sh` and the agent packages (amd64 and arm64, built into its image) and `token create` prints the complete install command, so a new server needs one pasted line and no package copied by hand. `install.sh` checks the CA fingerprint before it trusts the package, and `--deb` is now optional.
- New settings `TALAIA_DIST_DIR` and `TALAIA_PUBLIC_PORT` (`dist_dir`, `public_port`); the public port also feeds the default allowed origins, which now work with any published port.
- `packaging/core/install.sh`: sets up the core in one command (random database password in `.env`, build, start, administrator), and the installation guide `docs/en/installation.md`.
- CI job `core stack (docker)`: runs the core installer and checks that the core serves the panel, the CA, `install.sh` and both agent packages, that `token create` prints the install command, and that a server installs its agent with it and enrolls.
- API `POST /api/v1/servers/enrollment-tokens` (full session): creates a 15-minute, single-use enrollment token for a server name and returns the install command; audited as `enrollment_token_created`.
- `talaia-core server list` prints the enrolled servers with their ids, which `server revoke` needs.
- The install command now includes the SHA-256 of `install.sh` and refuses to run a script that does not match, so a machine in the middle cannot swap the script that is fetched before the CA is trusted.
- Installation guide section on reaching the panel from outside through a private VPN.
- "Add server" button on the fleet screen: asks for a name, creates the one-time token and shows the install command with a copy button, in ca/es/en. The empty-fleet message now points to it.
- Minimal agent link: the core hub accepts the agent stream (plaintext HTTP/2 for now) and the agent sends its inventory, heartbeats and CPU metrics, reading `/etc/talaia/agent.toml`.
- i18n for Go components (go-i18n in core and CLI, ca/es/en) and `scripts/i18n-check.mjs`, which fails when a key is missing in any language.
- `make dev`: local development environment (PostgreSQL + TimescaleDB in Docker Compose, core, agent and web dev server).
- GitHub Actions CI: Go build/vet/test, golangci-lint, buf lint and generated-code check, web build/lint/format/test and the i18n check.
- Live agent metrics (CPU, RAM, root disk, network counters), kept by the core hub.
- Agent `.deb` package (nfpm), systemd unit and `install.sh` (`scripts/build-deb.sh`). Enrollment is not implemented yet.
- Fleet screen (mockup 1) with sample data behind an API-client interface, and Fleet/About navigation.
- Agent inventory of Docker containers, Compose projects and systemd services, re-sent only when it changes.
- Core enrollment foundation: internal CA (ECDSA P-256), `server` and `enrollment_token` tables, and the commands `talaia-core token create` and `talaia-core server revoke`.
- Core over TLS 1.3 with mutual authentication: `GET /ca.pem`, `POST /enroll`, `POST /renew`, agent identity taken from the client certificate, and a plaintext `insecure_dev` mode for development.
- Agent enrollment with a one-time token pinned to the CA fingerprint, mTLS link and certificate renewal; `install.sh` now takes `--token` and `--ca-fingerprint`.

### Security

- The OVH credential documented for ACME is limited to the three calls lego makes on one zone, instead of every zone of the account. The installer refuses host names and ACME emails with characters that would break `.env`.
- The Content-Security-Policy now allows `<style>` elements from the page (`style-src-elem 'self' 'unsafe-inline'`), because xterm.js writes the colours of the terminal that way and, without it, the terminal shows dark text on a dark background. Scripts, inline style attributes, third-party origins and `data:` URLs stay blocked, and a test guards that nothing else is loosened.
- Security review of the enrollment: revocation now closes live agent streams, the agent identity is bound to the current or previous certificate serial, enrollment is a single transaction, the agent key and certificate are stored in one atomic file, `insecure_dev` only listens on loopback, permanent agent errors stop systemd restarts (exit 78), and the CA and certificate directories are forced to mode 0700.
- Panel authentication (ahead of phase 3): single administrator created from the console, argon2id password, mandatory TOTP with single-use recovery codes, secure session cookie, CSRF protection, brute-force limits, audit log, and the sign-in screens in ca/es/en.
- Core deployment with Docker Compose (`packaging/core/`): one image with the core and the web panel, PostgreSQL 16 + TimescaleDB, read-only root filesystem and no capabilities.
- `GET /api/v1/servers` (full session required) and the fleet screen fed by it, refreshed every 15 seconds; data that does not exist yet is shown as not available instead of sample numbers.
- Security review of the jobs and stacks: the agent writes stack files through an exclusive temporary file, so a symbolic link planted in the directory of an adopted project can no longer make it overwrite another file as root; reading an adopted stack refuses symbolic links; adoption refuses a compose file that is a link and files or directories other users can write; a new stack can no longer take the name of a running project Talaia does not manage (its `up --remove-orphans` would remove that project's containers).

### Changed

- `packaging/core/install.sh` chooses the ports itself: the first free one from 8443 (panel) and 8444 (agents), checked with `ss`, never 80 or 443 unless asked for; a port given with `--port` or `--agent-port` that is busy stops the script. It asks for the public name in a terminal and, with `--acme ovh` or on request, stores the OVH credentials in `.env.acme` (mode 0600) for the ACME certificate that comes next (`TALAIA_ACME_PROVIDER`, `TALAIA_ACME_EMAIL`; the core ignores them for now). The CI occupies 8443 and checks that the installer picks other ports.
- The core listens on two ports: the panel (`panel_listen` / `TALAIA_PANEL_LISTEN`, public port `panel_port`) serves the panel, the API and the terminal WebSocket, and the agent endpoint (`agent_listen` / `TALAIA_AGENT_LISTEN`, public port `agent_port`) serves the agent stream, `/enroll`, `/renew`, `/ca.pem`, `/install.sh` and `/agent/*`. Neither answers the other's routes. `listen`, `public_port`, `TALAIA_LISTEN` and `TALAIA_PORT` are replaced by these settings; the install command printed for agents uses the agent port and the allowed origins use the panel one. Compose publishes both ports (8443 and 8444 by default) and `packaging/core/install.sh` gets `--agent-port`; `--port` now defaults to 8443.
- New look for the panel: self-hosted fonts (Familjen Grotesk, IBM Plex Sans, JetBrains Mono, which were declared but never loaded), a sidebar layout that folds into a top bar on phones, status beacons, segmented gauges, a new watchtower logo, a redesigned sign-in with a light beam, a favicon, and motion that respects `prefers-reduced-motion`. The fleet no longer shows an empty activity panel. No image is a `data:` URL, so the panel works under its strict CSP.
- Phase 2 (started): typed container actions end to end. The contract gets `ContainerAction` and `JobEvent`; the agent runs start, stop, restart and remove through the Docker Engine API (validated references, no shell, repeated job ids answered with the first outcome); the core queues commands per agent, stores jobs (`job` table, states only move forward) and exposes `GET /api/v1/servers/{id}/containers`, `POST /api/v1/servers/{id}/containers/{container}/actions` and `GET /api/v1/jobs/{id}`. Removing needs an explicit `confirm`, only containers the agent reported can be acted on, and every request is audited.
- Container logs: the contract gets `ReadLogs` and `JobEvent.output`; the agent reads the last lines (1 to 1000, stdout and stderr, timestamped, bounded to 256 KiB) through the Docker Engine API; the core exposes `POST /api/v1/servers/{id}/containers/{container}/logs` and returns the text in the output of the job. Only the fact of reading is audited, never the text.
- Compose stacks managed by Talaia, in `/opt/talaia/stacks/<name>/` (`compose.yaml` plus an optional `.env` with mode 0600). The agent validates a project with `docker compose config` before it replaces any file, writes atomically, refuses symbolic links and runs docker compose with fixed arguments and a reduced environment (no shell); its inventory lists the managed stacks. The core exposes `GET /api/v1/servers/{id}/stacks`, `PUT …/stacks/{name}`, `POST …/stacks/{name}/read` and `POST …/stacks/{name}/actions` (up, down, pull, restart, remove; down and remove need `confirm`). The audit log keeps only the hashes of the files, never their contents.
- Live use of the containers: the agent samples `docker stats` for every running container every 15 seconds on its own timer (CPU as docker stats computes it, memory without reclaimable cache, network bytes) and sends it; the core keeps the last sample and shows it in the container list only while it is fresh (under a minute) and the container runs; the server screen draws a CPU and a RAM gauge in each running container.
- "Images, volumes and networks" section on the server screen: tabs per kind, each resource marked in use, unused or system, the space the unused images would free, a **Select what is unused** shortcut, and a confirmation (with a warning about data for volumes) before anything is removed; the ones Docker refuses are listed afterwards. In ca/es/en.
- Docker resources: the agent lists images, volumes and networks with whether a container uses each (system networks marked) and removes them one by one, never with force, so Docker itself refuses what is in use; the core exposes `POST /api/v1/servers/{id}/resources/list` and `…/resources/remove` (explicit `confirm`, at most 200 per request, ids validated per kind, every removal audited with what was asked).
- Adopting running Compose projects: `POST /api/v1/servers/{id}/stacks/{name}/adopt` and the `forget` stack action. The agent finds the project among its containers and records where its single compose file and directory are; nothing is copied or restarted, so relative paths and data keep working, and compose runs against the original files. Removing an adopted stack is refused; forgetting it deletes only the record. The core sends the name only, never a path.
- Running projects block on the stacks section: the Compose projects Talaia does not manage yet, each with a **Manage with Talaia** button; adopted stacks carry an *adopted* tag and offer **Stop managing** (with confirmation) instead of removing. In ca/es/en.
- Stacks section on the server screen: the managed stacks with the state of their containers, start, pull, restart, stop and remove (stop and remove ask first), a **New stack** editor and an **Edit** editor for `compose.yaml` and `.env` with **Validate and save** and **Save and start**. A rejected project shows the message of docker compose and keeps the editor open. In ca/es/en.
- Log viewer on the server screen: a **Logs** button per container opens its last lines in a dark console (refresh and close), in ca/es/en.
- `install.sh --upgrade` and `talaia-core server upgrade-command`: update the agent already installed on a server (same hash-checked download, keeps its configuration and identity, no token).
- Server screen: click the name of a server on the fleet to see its containers (state, image, stack) and start, stop, restart or remove them. Every action is a job the screen follows until it ends and then reloads the list; removing asks for confirmation in the row. The agent resends its inventory as soon as a job finishes. In ca/es/en.
- Phase 0 decisions closed: PostgreSQL + TimescaleDB, React + TypeScript, Go 1.26, Node.js 24 LTS with pnpm.
- The session that waits for the second factor lasts 15 minutes at the first access (it was 5), enough to install an authenticator app and scan the QR code; users that already have the second factor keep 5 minutes. The sign-in screen now says when the session expired.

### Fixed

- An agent that already had an identity from another core ignored the new enrollment token and kept failing to connect. When `install.sh` leaves a token and the stored CA does not match the configured fingerprint, the agent discards the old identity and enrolls again.
- Installer: rerunning it no longer takes its own `acme-dns` container, left running by an earlier run, for a process that holds port 53.
- `docker compose up --build` with the `acme-dns` profile failed on a clean machine with "pull access denied for talaia-core": the ACME DNS service uses the core image but did not say how to build it. Both services now share the build, and the CI starts the stack with that profile from nothing.
- ACME: the account and the certificate are stored per ACME directory (`<data_dir>/acme/<host>/`), so staging and production never mix and an account registered in one is not reused in the other.
- Update check: a commit whose CI was still running at the first check was never offered afterwards (the ETag of the head made every later check a 304), and a build newer than the newest green commit was offered a downgrade. The panel opened by IP address with ACME enabled now gets the certificate of the own CA, which covers it, instead of the public one.
- A stack action that takes more than two minutes (a slow `pull` or `up`) is no longer reported as timed out while it is still running: each kind of job now waits a little longer than the agent gives it.
- A job whose output or message was cut in the middle of a character is no longer lost: the agent and the core keep the text valid UTF-8.
- Validating an edited adopted stack with an empty `.env` no longer uses the old `.env` of its directory, which saving deletes.
- The core no longer crashes on start when `TALAIA_PUBLIC_HOSTNAMES` holds only IP addresses.
- The shell scripts (`packaging/install.sh`, `scripts/*.sh`) are now stored as executable, so `./install.sh` works after a clone.

## [0.0.1] - 2026-10-18

### Added

- Project definition: name, goals, two levels of management (container and whole server).
- README in English and Spanish.
- Roadmap with 10 phases and version mapping (ROADMAP, EN/ES).
- TODO list for the project definition and phase 0 (EN/ES).
- CONTRIBUTING and SECURITY guides (EN/ES).
- Architecture, i18n and development guides in `docs/en/` and `docs/es/`.
- Roadmap page with five mockups in `docs/design/talaia-roadmap.html`.
- Instructions for AI agents: `CLAUDE.md` (orchestrator), `AGENTS.md` (executors).
- VS Code workspace settings, recommended extensions, `.editorconfig`, `.gitattributes` and
  `.gitignore`.
- WNCL-1.0 license applied: `LICENSE` file and credits section in the READMEs.

[Unreleased]: https://github.com/catlinux/Talaia/compare/v0.3.5...HEAD
[0.3.5]: https://github.com/catlinux/Talaia/releases/tag/v0.3.5
[0.3.4]: https://github.com/catlinux/Talaia/releases/tag/v0.3.4
[0.3.3]: https://github.com/catlinux/Talaia/releases/tag/v0.3.3
[0.3.2]: https://github.com/catlinux/Talaia/releases/tag/v0.3.2
[0.3.1]: https://github.com/catlinux/Talaia/releases/tag/v0.3.1
[0.3.0]: https://github.com/catlinux/Talaia/releases/tag/v0.3.0
[0.2.15]: https://github.com/catlinux/Talaia/releases/tag/v0.2.15
[0.2.14]: https://github.com/catlinux/Talaia/releases/tag/v0.2.14
[0.2.13]: https://github.com/catlinux/Talaia/releases/tag/v0.2.13
[0.2.12]: https://github.com/catlinux/Talaia/releases/tag/v0.2.12
[0.2.11]: https://github.com/catlinux/Talaia/releases/tag/v0.2.11
[0.2.10]: https://github.com/catlinux/Talaia/releases/tag/v0.2.10
[0.2.9]: https://github.com/catlinux/Talaia/releases/tag/v0.2.9
[0.2.8]: https://github.com/catlinux/Talaia/releases/tag/v0.2.8
[0.2.7]: https://github.com/catlinux/Talaia/releases/tag/v0.2.7
[0.2.6]: https://github.com/catlinux/Talaia/releases/tag/v0.2.6
[0.2.5]: https://github.com/catlinux/Talaia/releases/tag/v0.2.5
[0.2.4]: https://github.com/catlinux/Talaia/releases/tag/v0.2.4
[0.2.3]: https://github.com/catlinux/Talaia/releases/tag/v0.2.3
[0.2.2]: https://github.com/catlinux/Talaia/releases/tag/v0.2.2
[0.2.1]: https://github.com/catlinux/Talaia/releases/tag/v0.2.1
[0.0.1]: https://github.com/catlinux/Talaia/releases/tag/v0.0.1
