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

## [0.6.0] - 2026-10-28

### Changed

- Phase 5 (monitoring, alerts, checks and notification channels) is closed after a real test: a stopped container reached Telegram as a firing alert and as a resolved one. The README, the roadmap and the TODO reflect it.

## [0.5.20] - 2026-10-28

### Changed

- The Settings screen is organised in tabs: **Account** (trusted devices and passkeys), **Servers**, **Alerts**, **Integrations** (single sign-on and API keys) and **System** (version and updates). Each user only sees the tabs they may use, the last one is remembered, and the progress of a running update stays visible above the tabs.

## [0.5.19] - 2026-10-28

### Fixed

- Metric consolidation from 5 minutes to 1 hour failed every few minutes with `column "cpu_pct" does not exist`, so the hourly tables (server and container) were never filled and the log filled with warnings. The step now reads the columns of the 5-minute tables; an hour is the average of its 5-minute averages (the rows carry no sample count to weight by) and the maximum of their maxima. The hourly buckets fill in from the 5-minute data that already exists.

## [0.5.18] - 2026-10-25

### Added

- Standard alert rules: CPU usage (90 % for 10 minutes), memory usage (90 % for 10 minutes) and a disk warning (80 %) join the built-in rules, so every server, also the ones added later, is covered by default. An update only adds the ones that are missing and never touches rules you edited.
- Per-server alert settings: a narrower rule now replaces a broader rule of the same type and severity for what it matches, even when it is disabled, and an alert open from the replaced rule is resolved. The server screen has an **Alerts** tab to customise a rule for that server, turn it off there or restore the default.

### Changed

- The monitoring guide has a quick start that walks through creating a Telegram bot, a channel and checking the rules.

## [0.5.17] - 2026-10-25

### Added

- Alert history is now bounded: resolved incidents are deleted after 180 days and finished delivery records after 30 (open incidents and pending retries are never touched).
- Email channels can use an SMTP relay without login (username and password are optional, but both or none); plain text is still never used.

### Security

- The outbound guard also blocks the rarely used IPv6 special ranges (IPv4-compatible `::/96`, `100::/64`, `2001::/23`, local-use NAT64, site-local and the new documentation block).

## [0.5.16] - 2026-10-25

### Fixed

- The text of the alert notifications read like a template ("Container down on X. name <no value> stack"): all of them are rewritten in Catalan, Spanish and English as clear sentences ("Container shop-web-1 is down on vps-web-01", "The root disk of vps-web-01 is 93 % full"), and an alert of a check shows the name of the check instead of its internal id.
- CI: the core lint job failed on the version of the linter that GitHub uses.

## [0.5.15] - 2026-10-25

### Added

- Documentation of phase 5: the new guide [docs/en/monitoring.md](docs/en/monitoring.md) (metrics and retention, alerts, silences, checks, notification channels, the outbound guard, permissions, API and a step-by-step test), and updates to the permissions, installation, security and architecture guides and the README, including the new requirement of PostgreSQL with TimescaleDB.

## [0.5.14] - 2026-10-25

### Security

- Email notifications escape lines that start with a dot, so the text of an alert can never end the message early and inject SMTP commands.
- The Telegram bot token must have the shape of a real token, so it cannot alter the request path to `api.telegram.org`.
- Request bodies of the rules, checks and silences endpoints are size-limited like the rest of the API.

### Fixed

- Discord and webhook channels could not be created: their URL, which is a secret, was validated from the public configuration.
- Email channels never sent anything: the SMTP port did not reach the sender. Named addresses (`Ops <ops@example.com>`) are now accepted.
- Two servers with a container of the same name shared one incident: recovering on one resolved the alert of the other.
- After "stop watching", a stale open incident could hide the next failure of the same container; a container whose stop from Talaia failed stayed unwatched for good (now it is watched again after 2 minutes).
- CPU, memory and disk alerts no longer resolve and fire again just because an agent disconnected.
- A brief database error no longer stops the checks runner until the core is restarted.
- Malformed ids in the alerts API answer 404 instead of a server error.
- Lint errors of the core that made CI fail since 0.5.5.

## [0.5.13] - 2026-10-24

### Added

- **Alert settings in the panel**: a new *Alerts* page in Settings (administrators only) to manage the alert rules (the built-in ones can be edited and disabled but not deleted), the HTTP/TCP/TLS checks (with *Run now* and the last result) and the notification channels (Telegram, ntfy, email, Discord, webhook) with a *Send test* button. Channel secrets are write-only: once saved they are never shown again, only replaced. It also holds the switch that lets checks and notifications reach private networks.

## [0.5.12] - 2026-10-24

### Added

- **Alerts screen in the panel**: a new *Alerts* entry in the navigation with a counter that turns red when something is critical, and a screen with *Active*, *History* and *Silences* tabs. Each alert is explained in your language (for example "Container shop-web-1 is stopped on vps-web-01") with how long ago it started, and, according to your permissions, you can acknowledge it, silence it (30 minutes up to 30 days, with a reason) or stop watching a container. On the fleet, the server cards of servers with active alerts get a red or amber edge and there is a new *Active alerts* indicator.

## [0.5.11] - 2026-10-24

### Added

- **Alerts are live in the core.** The alert engine, the checks runner and the notification worker start with the core, and the API serves them: `GET /api/v1/alerts` (active and history, filtered by what each user may see), `/alerts/summary`, acknowledge and stop-watching a container (`/alerts/{id}/ack`, `/unwatch`), silences (`/silences`, up to 30 days, with a reason), alert rules, HTTP/TCP/TLS checks (with run-now), notification channels with a test message, and the setting that allows private targets. Rules, checks, channels and the setting need the new `alerts.manage` permission (administrators only, never API keys); acknowledging and silencing need `alerts.silence`. Everything is audited and channel secrets are never returned. Without the panel screens (next steps) they are managed through the API.

## [0.5.10] - 2026-10-24

### Added

- Notification channels and delivery queue worker (not running yet): Telegram, ntfy, email (STARTTLS required or implicit TLS, never in clear), Discord and signed webhook (`X-Talaia-Signature`, HMAC-SHA256). Secrets are kept sealed and write-only, errors never contain URLs, tokens or passwords, every message is sent in the language of the channel (ca/es/en), failed deliveries are retried after 30 s, 2 min, 5 min and 15 min and a test message can be sent to a channel.

## [0.5.9] - 2026-10-24

### Added

- Checks runner (not running yet): HTTP, TCP and TLS checks run from the core through the outbound guard every 30 seconds at least (8 at a time, never overlapping themselves), at most 3 redirects, a body is never kept, and they record latency, status code and the certificate expiry date even when the certificate is not valid. Stored errors are short fixed messages that never contain the URL or the response.

## [0.5.8] - 2026-10-24

### Added

- Alert engine (not running yet): evaluates the alert rules every 5 seconds and when an inventory, a disconnection or a check result arrives, and keeps incidents with a pending period, hysteresis (a threshold alert resolves 2 points below), a 10-minute cooldown against flapping, optional repetition until acknowledged, a 90-second grace period after the core starts and silences (which still record the incident). A container the core saw running and that disappears or stops raises an alert, except when it was stopped, brought down or removed from Talaia, and rebooting a server from Talaia silences it for 10 minutes. It queues one delivery per target channel; sending them comes in a later step.

## [0.5.7] - 2026-10-24

### Security

- Outbound traffic guard (not used yet): the checks and notifications of the next steps will connect through a dialer that validates the IP address actually being connected to, so a name that resolves to a private address (DNS rebinding) or a redirect cannot reach loopback, private networks, CGNAT, link-local or cloud metadata addresses (`169.254.169.254` stays blocked even if private targets are allowed), including their IPv4-mapped, NAT64, 6to4 and Teredo forms. It ignores proxy environment variables, follows at most 3 redirects and reads at most 64 KiB.

## [0.5.6] - 2026-10-24

### Added

- History charts in the panel: the **Overview** tab of a server shows CPU, memory, disk and network over 1 h, 6 h, 24 h, 7 d or 30 d (refreshed every 30 s, with the current value, a tooltip that follows the pointer or the keyboard, gaps where there is no data and a text summary for screen readers), and the stack screen shows a CPU and memory sparkline per container. The charts are drawn in SVG by the panel itself, in ca/es/en and in the light and dark themes. Users who cannot see a server's metrics do not see the section.

## [0.5.5] - 2026-10-24

### Added

- The core now records the metrics of every server and container (CPU, memory, disk, network rate computed from the counters) and serves them at `GET /api/v1/servers/{id}/metrics` and `GET /api/v1/servers/{id}/containers/{name}/metrics` (`range=1h|6h|24h|7d|30d` or `from`/`to`; resolution chosen by range, at most 500 points). They need `server.view` and `container.view`, so a user limited to a stack only sees the containers they may see. Removing a server deletes its metrics.

## [0.5.4] - 2026-10-24

### Added

- Alerts data layer (not reachable yet): permissions `alerts.view` (every role), `alerts.silence` (admin, server operator, app operator) and `alerts.manage` (admin only; API keys can never have it), and migration 0015 with the tables for rules (with the default rules: server offline 45 s, disk above 90 %, container down 20 s, container unhealthy 60 s, failed check, certificate expiring in 14 days), incidents (never two open incidents with the same fingerprint), watched containers, silences (30 days at most), HTTP/TCP/TLS checks, notification channels (secrets stored sealed and never read back) and a delivery queue with retries.

## [0.5.3] - 2026-10-24

### Added

- Metrics history storage (not recorded or shown yet): migration 0014 enables TimescaleDB and creates the tables for server and container samples at raw, 5-minute and 1-hour resolution, plus a buffered recorder, downsampling of closed buckets and retention done by the core itself (only Apache-licensed TimescaleDB functions). Retention is set in the new `[metrics]` section of the configuration file (defaults: server 48 h / 30 d / 400 d, container 24 h / 7 d / 90 d). **Talaia now needs PostgreSQL with the TimescaleDB extension**; the images in `packaging/` already include it.

## [0.5.2] - 2026-10-24

### Added

- The agent reports the health of each container (`healthy`, `unhealthy`, `starting`) and follows the Docker events (start, die, stop, kill, oom, destroy, health): a container that stops or fails its healthcheck reaches the core within about 2 seconds instead of up to a minute. If the event stream drops it reconnects with backoff; the 60-second inventory stays as a safety net.

## [0.5.1] - 2026-10-24

### Security

- Removing or forgetting a stack now needs `stack.edit`: an app operator (who only has `stack.deploy`) could delete the files of a stack they could not edit. Found in the phase 4 security review.
- Terminal recordings (`recordings.view`) are for administrators only: viewers and app operators on the whole organization could download every recording, including the output of root shells on the hosts.

### Fixed

- Permission docs: the app operator does not read the journal, users can be deleted once disabled, and the role tables reflect the two changes above.

## [0.5.0] - 2026-10-24

### Changed

- Phase 4 (users, permissions and audit) closed after a real test with a second user: roles by scope, teams and groups, invitations, API keys, OIDC, recordings and `talaiactl`. Guide: `docs/en/permissions.md`.

## [0.4.25] - 2026-10-24

### Added

- Panel: delete a disabled user (typing their name to confirm) and, in the new API key dialog, an example with the real key in two variants, Linux/macOS and PowerShell. The error of the access form no longer stays on screen after you change the form.

## [0.4.24] - 2026-10-24

### Added

- Delete users: `DELETE /api/v1/users/{id}` with the username as confirmation, only for disabled accounts, never yourself or the last administrator. It removes the account, its access, API keys and team memberships; the audit history keeps the name.

### Security

- `GET /api/v1/servers` no longer gives `agent_latest_version` and `agent_upgrade_command` to users who cannot view any server.

## [0.4.23] - 2026-10-24

### Fixed

- The Cancel button of the add-access form showed `common.cancel` instead of its text.

## [0.4.22] - 2026-10-24

### Added

- `talaia-core user unlock [--username <name>]` clears the lock left by too many failed sign-ins (the per-address limit lives in memory and is cleared by restarting the core).

### Fixed

- `talaia-core user reset-2fa` and `session revoke-all` acted on an arbitrary user when there were several: they now take `--username <name>`, `reset-2fa` refuses to guess, and `session revoke-all` without a name revokes everyone's sessions.

## [0.4.21] - 2026-10-23

### Fixed

- Code formatting of the panel (Prettier) after the latest changes.

## [0.4.20] - 2026-10-23

### Fixed

- The email field of the invitation is really optional, and the sign-in screen says when the account was disabled or the session revoked instead of "session expired". Messages for self-disable included.

## [0.4.19] - 2026-10-23

### Changed

- Disabling your own account answers `cannot_disable_self`, and a request made with the session of a disabled account or a revoked session answers `account_disabled` or `session_revoked` instead of `session_required`.

## [0.4.18] - 2026-10-23

### Changed

- Panel forms (invitations, access, teams, groups, API keys, single sign-on) check required fields before sending and show the problem under the field; the server's validation errors are explained field by field. The stack or container of an access is picked from the server's list (typing the name remains as a fallback).

## [0.4.17] - 2026-10-23

### Changed

- Validation errors of the administration API now name the failing field and the reason (`{"error":{"code":"invalid_request","field":"scope.name","reason":"required"}}`) instead of a bare `invalid_request`.

## [0.4.16] - 2026-10-23

### Fixed

- Linter findings of the phase 4 code (unchecked closes, empty branches, if-else chains) in the core and `talaiactl`.

## [0.4.15] - 2026-10-23

### Changed

- Documentation of users, permissions, API keys, OIDC, recordings and talaiactl.

## [0.4.14] - 2026-10-23

### Added

- `talaiactl` v1: `login`, `servers list`, `containers list|start|stop|restart`, `stacks list|up|down|restart|pull`, `jobs wait` and `version`, with an API key from `~/.config/talaia/config.toml` (mode 0600), `TALAIA_URL` / `TALAIA_API_KEY` or flags, table or `--json` output, an extra CA with `--ca-file` (TLS verification can never be switched off), and messages in ca, es and en.

## [0.4.13] - 2026-10-23

### Added

- Panel: API keys in Settings (created with a fresh TOTP code, shown once, with a ready-made `curl` example), single sign-on settings for administrators, a "Sign in with…" button on the login when OIDC is enabled, and a list of terminal recordings with `.cast` download in the audit screen.

## [0.4.12] - 2026-10-23

### Added

- Users and permissions screen: users, teams and server groups, role assignments by scope (organization, group, server, stack, container) with a plain description of what each role cannot do, disabling and enabling, and invitations by link (shown once, with the pending list). People accept an invitation on `/invite#…`, choose a password and set up the second factor. In ca, es and en.

## [0.4.11] - 2026-10-23

### Changed

- The panel reads the user's permissions (`/api/v1/me`) and hides what they cannot use: audit, settings, adding servers, host tabs, terminals, reboot, container and stack actions, compose editing. A user who only has a stack sees a minimal server card and only the Containers tab. The core still enforces everything.

## [0.4.10] - 2026-10-23

### Security

- `GET /api/v1/servers` now returns only the name and whether it is online for users who reach a server only through a stack or container assignment; the host details (system, usage, kernel, updates, agent) need `server.view`.

## [0.4.9] - 2026-10-23

### Added

- Terminal recordings API (permission `recordings.view`, never with API keys): `GET /api/v1/recordings` lists who opened which session, where, when and for how long, and `GET /api/v1/recordings/{id}/download` gives the `.cast` file for `asciinema play`. Downloads are audited.

## [0.4.8] - 2026-10-23

### Added

- OpenID Connect sign-in endpoints (`/api/v1/auth/oidc/config`, `/start`, `/callback`): the identity provider replaces the password only, the second factor (TOTP or passkey) is still required. There is no automatic account creation: the provider's verified email must match an existing active user, and the provider's `sub` is bound on the first login and is what counts from then on. Failures redirect to `/login?oidc_error=oidc_failed` without saying why and are audited as `oidc_login_denied`.

## [0.4.7] - 2026-10-23

### Added

- OpenID Connect client (not usable to sign in yet): discovery, authorization-code flow with PKCE, `state` and `nonce`, ID token verification, and the settings API `GET/PUT /api/v1/settings/oidc` (permission `settings.manage`; the client secret is stored encrypted and never returned). Only `https` issuers are accepted, except localhost. New dependency: `github.com/coreos/go-oidc/v3`.

## [0.4.6] - 2026-10-23

### Added

- API keys (`Authorization: Bearer tlk_…`) with their own role assignments: a key's permissions are the intersection of its assignments and its owner's current ones, so it loses access when the owner does. A key can never open terminals, manage users, teams, groups, keys or settings, reboot a server or reach the sign-in endpoints. They last 90 days by default (365 at most), are shown once and stored hashed, need a fresh TOTP code to be created, are limited to 600 requests per minute and are revoked when their owner is disabled. API: `POST/GET /api/v1/apikeys` and `DELETE /api/v1/apikeys/{id}`.

## [0.4.5] - 2026-10-23

### Added

- API for teams and server groups (permission `users.manage`): create, rename and delete them, add and remove team members, give teams role assignments, and put a server in a group so group-scoped assignments reach it. Removing the last administrator through a team is refused. Everything is audited.

## [0.4.4] - 2026-10-23

### Added

- User management API (permission `users.manage`): list users, edit name and email, disable and enable (the last administrator cannot be disabled, demoted or removed; nobody can disable themselves), create, change and delete role assignments, and invitations by link (72 hours, single use, the token is shown once and stored hashed). The invited person sets a password at `/api/v1/auth/invite/{token}/accept` and must enrol TOTP before getting a full session. Everything is audited.

## [0.4.3] - 2026-10-23

### Added

- Lists are filtered by permission (servers, containers, stacks), `GET /api/v1/stacks` lists the visible stacks of all servers, `GET /api/v1/me` returns the user and their effective permissions for the panel, and a job can only be read by its creator or an administrator. The audit log entries carry the actor id and scope. A test with a user who only has one stack checks every route and finds nothing else reachable.

## [0.4.2] - 2026-10-23

### Added

- Permissions are now enforced on every API route: each route declares its permission in one table (a test fails if a route has none), a user only sees servers, stacks and containers their assignments reach (404 otherwise, 403 when visible but not allowed), every denial is audited as `authz_denied`, and reading a stack's compose file needs the edit permission. Disabled users lose their session. The first administrator created at setup gets the organization admin role.

## [0.4.1] - 2026-10-22

### Added

- Groundwork for users and permissions (not reachable yet): the `rbac` package (fixed roles, scope levels and inheritance from organization down to container, so a role bound to a stack never reaches the host) and migration 0012 with server groups, teams, role bindings, user fields, invitations, API keys and the audit actor/scope. The store refuses to remove, disable or demote the last active administrator, and disabling a user revokes their sessions, trusted devices and API keys in the same transaction.

## [0.4.0] - 2026-10-22

### Changed

- **Phase 3 (whole server, MVP) closed.** systemd services, the journal with filters and follow, apt updates (security or all) with reboot confirmed by the server name, the host terminal with a fresh TOTP code, the server screen with tabs and the audit screen were tested on two real servers; the security review of the phase is done. The maintainer closed it after the real test; the week without SSH goes on in daily use. Phase 4 (users, permissions and audit) comes next.

## [0.3.23] - 2026-10-22

### Fixed

- Terminal: Ctrl+C copies the selected text (without a selection it is still the interrupt), Ctrl+V pastes, and Ctrl+Shift+C / Ctrl+Shift+V always copy and paste. Before, only the context menu worked.

## [0.3.22] - 2026-10-22

### Fixed

- The Journal filter bar is aligned: the fields with a help text below no longer push their inputs lower than the others.
- **Reload** is offered only to services that support it (systemd `CanReload`, reported by the agent as `can_reload`), instead of failing with "Job type reload is not applicable".
- The badge of the **Updates** tab follows the last status read (after *Check now* or an upgrade) instead of the figures loaded when the screen opened.
- The live log of an apt upgrade shows lines as apt-get writes them (line-buffered output) instead of staying empty for minutes.

## [0.3.21] - 2026-10-22

### Fixed

- A fresh agent install no longer fails to connect: the installer wrote `core_url` and `ca_fingerprint` at the end of `/etc/talaia/agent.toml`, which after the new `[terminal]` table hid them inside it. They now go at the top, the packaged `agent.toml` is back to its previous content, and the agent says clearly when those keys are inside `[terminal]`. Found by the CI install test.
- Installing or updating the agent package (from the panel or with the install command) keeps the edited `/etc/talaia/agent.toml` without asking. Before, a change of that file in a new package would have stopped dpkg at a question nobody can answer and left the agent half installed.
- The result of a job reaches the core even when the stream to the core was lost and reconnected while it ran (an apt upgrade can take long). Before, the core kept the job as running until its time limit.
- Service actions work for instances such as `getty@tty1.service`: the `@` the panel escapes in the address was rejected.

### Security

- A reboot can no longer start in the middle of an apt upgrade. If an upgrade outlives its job time limit, or an earlier agent process left one running, the agent keeps the apt lock and follows the unit until `apt-get` ends; until then a reboot or another upgrade is refused with *another apt operation is running*. Found in the security review of phase 3.
- A wrong TOTP code when opening a host terminal is now recorded in the audit log as `host_terminal_denied` (never the code). Found in the same review, which also covered the host terminal, service actions, the journal search and the audit log without other findings.

## [0.3.20] - 2026-10-22

### Added

- Guide to the whole-server screen (`docs/en/server.md`): updates and reboot, services, journal, host terminal and audit, with limits and troubleshooting. The security, architecture and installation docs describe what phase 3 can do as root, what is recorded (never the TOTP code, the search text, the journal lines or the package lists) and how to turn the host terminal off with `[terminal] host = false`.

## [0.3.19] - 2026-10-22

### Added

- The server screen is now organised in tabs: **Overview** (CPU, memory and disk gauges, a *Needs attention* list that jumps to the right tab — reboot required, security updates, full disk, failed services, agent update — and the facts of the machine), **Containers**, **Services**, **Logs** and **Updates**, with count badges. New **Audit** screen with every recorded action, newest first, filters by group of actions and user, details and *Load more*. In ca, es and en.

## [0.3.18] - 2026-10-22

### Added

- The core serves the audit log, read only: `GET /api/v1/audit` returns the newest events first with a cursor (`next`/`before`) and filters by action (exact or prefix such as `service_*`), server and user. Reading it is not audited. Not in the panel yet.

## [0.3.17] - 2026-10-22

### Added

- Terminal on the server itself from the panel: a **Terminal** button on the server screen opens a root shell after a confirmation and a fresh 6-digit TOTP code, with a permanent red *Root on <server>* banner and the recording mark; it is disabled with an explanation when the agent has it off or is too old. In ca, es and en.

## [0.3.16] - 2026-10-22

### Added

- The core opens a recorded root terminal on the server itself (`POST /api/v1/servers/{id}/terminal`): it needs `confirm`, a fresh TOTP code (verified with the same replay guard as login and password change, so a code cannot be reused) and an agent that has the host terminal enabled. Recorded like container terminals, audited as `host_terminal_open` and `host_terminal_close`, one per user and server, and shown as `host` in `talaia-core terminal list`. Not in the panel yet.

## [0.3.15] - 2026-10-22

### Added

- The agent can open a root shell on the server itself (not reachable from the panel yet): a login shell from a closed list (`bash` or `sh`) on a local pseudo-terminal with a clean environment, the same limits as container terminals plus at most 2 host sessions, and the whole process group is killed when the session ends. It can be turned off with `[terminal] host = false` in `agent.toml` (or `TALAIA_AGENT_TERMINAL_HOST`) and the agent then reports it in its inventory. New dependency: `github.com/creack/pty` (MIT).

## [0.3.14] - 2026-10-22

### Added

- Journal in the panel: the server screen has a **Journal** section with filters (service, priority, boot, time range, text search), a **Follow** mode that brings new lines every few seconds, highlighting of the searched text and a download button. In ca, es and en.

## [0.3.13] - 2026-10-22

### Added

- The core reads the system journal of a server (`POST /api/v1/servers/{id}/journal`) with validated filters: service, priority, time range, boot, text search and a cursor to follow. The audit log records that it was read, never the search text or the lines. Not in the panel yet.

## [0.3.12] - 2026-10-22

### Added

- The agent reads the system journal with filters (service, priority, time range, boot, literal text search) and a cursor to follow new lines (not reachable from the panel yet). The search text is always escaped and nothing is run through a shell.

## [0.3.11] - 2026-10-22

### Added

- Services in the panel: the server screen has a **Services** section with search, filters (active, failed, inactive), the state and the enabled state of each service, and start, stop, restart, reload, enable and disable (stop, restart and disable ask first, with a stronger warning for services that can cut your access such as SSH, Docker or the network). The agent service has no stop actions. In ca, es and en.

## [0.3.10] - 2026-10-22

### Added

- The core lists the systemd services of a server (`GET /api/v1/servers/{id}/services`) and runs `start`, `stop`, `restart`, `reload`, `enable` or `disable` on one (`POST …/services/{unit}/actions`; stop, restart and disable need `confirm`; only services the agent reported; the agent service cannot be stopped from the panel). Audited as `service_<action>`. Not in the panel yet.

## [0.3.9] - 2026-10-22

### Added

- The agent reports whether each service is enabled and can start, stop, restart, reload, enable or disable a service (not reachable from the panel yet). It refuses to stop, restart or disable itself.

## [0.3.8] - 2026-10-22

### Added

- Update and reboot from the panel: the **Updates** section has **Update security** and **Update all** buttons (confirmation first; packages are never removed, local configuration is kept, a warning appears when Docker would restart) with the live apt log while it runs, and a **Reboot** action that needs the server hostname typed. In ca, es and en.

## [0.3.7] - 2026-10-22

### Added

- The core can upgrade packages and reboot a server: `POST /api/v1/servers/{id}/apt/upgrade` (`mode`: `security` or `all`, needs `confirm: true`) and `POST /api/v1/servers/{id}/reboot` (the confirmation must be the hostname the agent reports, so it cannot reach the wrong machine). Only one upgrade or reboot per server at a time. Audited as `apt_upgrade` and `host_reboot`. Not in the panel yet.

## [0.3.6] - 2026-10-22

### Added

- The agent can upgrade packages and reboot (not reachable from the panel yet): `AptUpgrade` runs `apt-get` in a transient systemd unit with a log, so an upgrade survives an agent restart and the agent reports its result when it comes back; modes are *security only* (packages the agent works out itself) and *all* (never removes packages, keeps local configuration); only one apt operation at a time. `HostReboot` checks the server name and reboots five seconds after reporting success.

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

[Unreleased]: https://github.com/catlinux/Talaia/compare/v0.6.0...HEAD
[0.6.0]: https://github.com/catlinux/Talaia/releases/tag/v0.6.0
[0.5.20]: https://github.com/catlinux/Talaia/releases/tag/v0.5.20
[0.5.19]: https://github.com/catlinux/Talaia/releases/tag/v0.5.19
[0.5.18]: https://github.com/catlinux/Talaia/releases/tag/v0.5.18
[0.5.17]: https://github.com/catlinux/Talaia/releases/tag/v0.5.17
[0.5.16]: https://github.com/catlinux/Talaia/releases/tag/v0.5.16
[0.5.15]: https://github.com/catlinux/Talaia/releases/tag/v0.5.15
[0.5.14]: https://github.com/catlinux/Talaia/releases/tag/v0.5.14
[0.5.13]: https://github.com/catlinux/Talaia/releases/tag/v0.5.13
[0.5.12]: https://github.com/catlinux/Talaia/releases/tag/v0.5.12
[0.5.11]: https://github.com/catlinux/Talaia/releases/tag/v0.5.11
[0.5.10]: https://github.com/catlinux/Talaia/releases/tag/v0.5.10
[0.5.9]: https://github.com/catlinux/Talaia/releases/tag/v0.5.9
[0.5.8]: https://github.com/catlinux/Talaia/releases/tag/v0.5.8
[0.5.7]: https://github.com/catlinux/Talaia/releases/tag/v0.5.7
[0.5.6]: https://github.com/catlinux/Talaia/releases/tag/v0.5.6
[0.5.5]: https://github.com/catlinux/Talaia/releases/tag/v0.5.5
[0.5.4]: https://github.com/catlinux/Talaia/releases/tag/v0.5.4
[0.5.3]: https://github.com/catlinux/Talaia/releases/tag/v0.5.3
[0.5.2]: https://github.com/catlinux/Talaia/releases/tag/v0.5.2
[0.5.1]: https://github.com/catlinux/Talaia/releases/tag/v0.5.1
[0.5.0]: https://github.com/catlinux/Talaia/releases/tag/v0.5.0
[0.4.25]: https://github.com/catlinux/Talaia/releases/tag/v0.4.25
[0.4.24]: https://github.com/catlinux/Talaia/releases/tag/v0.4.24
[0.4.23]: https://github.com/catlinux/Talaia/releases/tag/v0.4.23
[0.4.22]: https://github.com/catlinux/Talaia/releases/tag/v0.4.22
[0.4.21]: https://github.com/catlinux/Talaia/releases/tag/v0.4.21
[0.4.20]: https://github.com/catlinux/Talaia/releases/tag/v0.4.20
[0.4.19]: https://github.com/catlinux/Talaia/releases/tag/v0.4.19
[0.4.18]: https://github.com/catlinux/Talaia/releases/tag/v0.4.18
[0.4.17]: https://github.com/catlinux/Talaia/releases/tag/v0.4.17
[0.4.16]: https://github.com/catlinux/Talaia/releases/tag/v0.4.16
[0.4.15]: https://github.com/catlinux/Talaia/releases/tag/v0.4.15
[0.4.14]: https://github.com/catlinux/Talaia/releases/tag/v0.4.14
[0.4.13]: https://github.com/catlinux/Talaia/releases/tag/v0.4.13
[0.4.12]: https://github.com/catlinux/Talaia/releases/tag/v0.4.12
[0.4.11]: https://github.com/catlinux/Talaia/releases/tag/v0.4.11
[0.4.10]: https://github.com/catlinux/Talaia/releases/tag/v0.4.10
[0.4.9]: https://github.com/catlinux/Talaia/releases/tag/v0.4.9
[0.4.8]: https://github.com/catlinux/Talaia/releases/tag/v0.4.8
[0.4.7]: https://github.com/catlinux/Talaia/releases/tag/v0.4.7
[0.4.6]: https://github.com/catlinux/Talaia/releases/tag/v0.4.6
[0.4.5]: https://github.com/catlinux/Talaia/releases/tag/v0.4.5
[0.4.4]: https://github.com/catlinux/Talaia/releases/tag/v0.4.4
[0.4.3]: https://github.com/catlinux/Talaia/releases/tag/v0.4.3
[0.4.2]: https://github.com/catlinux/Talaia/releases/tag/v0.4.2
[0.4.1]: https://github.com/catlinux/Talaia/releases/tag/v0.4.1
[0.4.0]: https://github.com/catlinux/Talaia/releases/tag/v0.4.0
[0.3.23]: https://github.com/catlinux/Talaia/releases/tag/v0.3.23
[0.3.22]: https://github.com/catlinux/Talaia/releases/tag/v0.3.22
[0.3.21]: https://github.com/catlinux/Talaia/releases/tag/v0.3.21
[0.3.20]: https://github.com/catlinux/Talaia/releases/tag/v0.3.20
[0.3.19]: https://github.com/catlinux/Talaia/releases/tag/v0.3.19
[0.3.18]: https://github.com/catlinux/Talaia/releases/tag/v0.3.18
[0.3.17]: https://github.com/catlinux/Talaia/releases/tag/v0.3.17
[0.3.16]: https://github.com/catlinux/Talaia/releases/tag/v0.3.16
[0.3.15]: https://github.com/catlinux/Talaia/releases/tag/v0.3.15
[0.3.14]: https://github.com/catlinux/Talaia/releases/tag/v0.3.14
[0.3.13]: https://github.com/catlinux/Talaia/releases/tag/v0.3.13
[0.3.12]: https://github.com/catlinux/Talaia/releases/tag/v0.3.12
[0.3.11]: https://github.com/catlinux/Talaia/releases/tag/v0.3.11
[0.3.10]: https://github.com/catlinux/Talaia/releases/tag/v0.3.10
[0.3.9]: https://github.com/catlinux/Talaia/releases/tag/v0.3.9
[0.3.8]: https://github.com/catlinux/Talaia/releases/tag/v0.3.8
[0.3.7]: https://github.com/catlinux/Talaia/releases/tag/v0.3.7
[0.3.6]: https://github.com/catlinux/Talaia/releases/tag/v0.3.6
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
