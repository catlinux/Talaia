# Architecture

[Leer en castellano](../es/arquitectura.md) · [Back to README](../../README.md)

> Status: **design**. This document describes the target architecture. It is updated as each phase is
> implemented.

## 1. Overview

```
 Browser (talaia-web) ─┐
 talaiactl ────────────┼── HTTPS: REST + WebSocket ──▶ ┌────────────────────┐
 Git webhooks ─────────┘                               │    talaia-core     │──▶ PostgreSQL 16
                                                       │                    │    + TimescaleDB
                                                       └─────────▲──────────┘
                                                                 │ gRPC bidirectional stream
                                                                 │ mTLS, TCP 443
                                                                 │ (opened by the agent)
                                         ┌───────────────────────┼───────────────────────┐
                                   talaia-agent            talaia-agent             talaia-agent
                                   (vps-web-01)            (vps-db-01)              (rpi-home)
                                   Docker · systemd · journald · apt · metrics · PTY · Caddy · restic
```

## 2. Components

### 2.1 talaia-core

Single Go binary. Deployed with a `compose.yaml` (core + PostgreSQL) on a small VPS or a home server.

| Module | Responsibility |
|---|---|
| `api` | REST + WebSocket API for the web panel and the CLI |
| `auth` | Sessions, TOTP, WebAuthn passkeys, OIDC, API keys |
| `rbac` | Roles and scopes (organization → group → server → stack → container) |
| `hub` | gRPC server; keeps one stream per connected agent; routes jobs and events |
| `jobs` | Persistent job queue (River on PostgreSQL), retries, scheduling |
| `alerts` | Evaluates rules on metrics and events, sends notifications |
| `deploy` | Git sources, builds, deploy pipeline, rollbacks |
| `secrets` | Encrypts secret variables with `age`; decrypted only by the target agent |
| `ca` | Internal certificate authority for agent certificates |
| `audit` | Append-only log of every action: who, what, where, from which IP, result |
| `store` | Database access (sqlc) and migrations |

The core also serves the compiled web panel as static files.

### 2.2 talaia-agent

Static Go binary (~15 MB), distributed as a `.deb` package with a systemd unit.
Targets: < 30 MB RAM and < 1 % CPU when idle.

| Module | Uses | Does |
|---|---|---|
| `docker` | Docker Engine API (socket) | Containers, images, volumes, networks, stats, exec |
| `compose` | `docker compose` v2 | Stacks in `/opt/talaia/stacks/<name>/` |
| `systemd` | D-Bus (go-systemd) | List, start, stop, enable units |
| `journal` | sdjournal | Read and follow logs with filters |
| `apt` | apt / dpkg | Pending and security updates, upgrade, reboot-required |
| `metrics` | /proc, gopsutil | CPU, RAM, disk, network, per-container stats |
| `pty` | creack/pty | Interactive terminals (host or container) |
| `files` | filesystem | Browse, read, edit (within allowed paths) |
| `firewall` | nftables / ufw | Read and change rules |
| `backup` | restic | Run backup plans and restores |
| `proxy` | Caddy admin API | Domains, TLS, routes |

Every module can be disabled in `/etc/talaia/agent.toml`. The agent keeps 24 h of metrics and events in
a local buffer and sends them when it reconnects.

### 2.3 talaia-web

React + TypeScript (Vite) single-page application. TanStack Query for data, WebSocket for live updates,
xterm.js for terminals, Monaco for editors, uPlot for charts, i18next for translations (ca/es/en).

### 2.4 talaiactl

Go command-line client for scripts, CI pipelines and GitOps sync. It authenticates with an API key
(`Authorization: Bearer tlk_…`) and exposes `login`, `servers list`,
`containers list|start|stop|restart`, `stacks list|up|down|restart|pull`, `jobs wait`, `version` and
`help`. It reads `url` and `api_key` from `~/.config/talaia/config.toml` (the directory returned by
`os.UserConfigDir()`; `%AppData%\talaia\config.toml` on Windows), created with mode 0600 and refused on Unix if group
or other users can read it. `TALAIA_URL`, `TALAIA_API_KEY`, `TALAIA_CA_FILE` and the matching flags
override the file. Output is an aligned table by default or `--json` raw data; actions can use
`--wait`, and `jobs wait` polls every 2 seconds up to `--timeout` (10 minutes by default). TLS uses the
system roots plus an optional `--ca-file`; verification cannot be disabled.

## 3. Agent ↔ core connection

### 3.1 Enrollment

1. The user clicks **Add server** in the panel. The core creates a one-time token valid for 15 minutes.
2. On the VPS: the command the panel shows (it downloads `install.sh` from the core and checks it) or, alternatively,
   `apt install talaia-agent` + `talaia-agent enroll`.
3. The agent generates a private key that never leaves the server and sends a CSR with the token.
4. The core's internal CA signs a 90-day certificate bound to the server ID. It renews automatically at
   two thirds of its life.
5. The agent opens a persistent gRPC stream over mTLS to port 443 of the core.
6. The agent sends its initial inventory and the server appears in the fleet.

Implemented so far (phase 1, design in `.agents/plans/fase1/D-enrolamiento.DESIGN.md`, pending review):

- The token is created from the panel (**Add server**, `POST /api/v1/servers/enrollment-tokens`, full session
  only, audited) or from the console with `talaia-core token create --name <server>`. Both give the **CA
  fingerprint** (SHA-256) and a ready-to-paste command; `install.sh` takes `--url`, `--token` and
  `--ca-fingerprint`.
- The core serves `install.sh` and the agent packages (`/install.sh`, `/agent/talaia-agent_<arch>.deb`, built
  into its image). The command downloads the script before the server trusts the CA, so it carries the SHA-256
  of the script and refuses to run a different one; the package is then downloaded over a connection that trusts
  the CA alone, after the fingerprint check.
- Trust anchor: the agent downloads `GET /ca.pem` once without verification and continues **only if** its
  SHA-256 matches the fingerprint; everything after that is verified TLS 1.3.
- The token is stored hashed, expires in 15 minutes, is consumed atomically and a rejected token never says
  why (unknown, used or expired). `POST /enroll` is limited to 10 requests per minute per IP.
- The CA ignores the subject of the CSR, requires an ECDSA P-256 key and sets the common name to the server
  ID. The core takes the agent identity from that certificate; `talaia-core server revoke <id>` blocks it on
  the next connection or renewal.
- `insecure_dev` (core) and `TALAIA_AGENT_INSECURE_DEV` (agent) switch to a plaintext link for local
  development only; `make dev` uses them.

### 3.2 Protocol

Contract in `proto/agent.proto` (managed with `buf`). Summary:

```proto
service AgentHub {
  rpc Connect(stream AgentMsg) returns (stream CoreMsg);
}

message CoreMsg {               // core → agent
  string job_id = 1;            // idempotency key
  oneof cmd {
    ComposeUp   compose_up = 10;
    ExecPty     exec_pty   = 11;
    AptUpgrade  apt        = 12;
    ReadLogs    logs       = 13;
    // one message per typed command
  }
}

message AgentMsg {              // agent → core
  oneof msg {
    Heartbeat heartbeat = 1;    // every 10 s
    Metrics   metrics   = 2;    // every 15 s
    JobEvent  job_event = 3;    // progress, output, result
    Inventory inventory = 4;    // on connect and on change
  }
}
```

Principles:

- **Typed commands only.** No arbitrary shell, except interactive terminals that the user opens
  explicitly and that are recorded.
- **Idempotent jobs.** Every command has a `job_id`; repeating it does not repeat the effect.
- **Reconnection** with exponential backoff and jitter (1 s → 60 s).
- **Versioned contract.** Fields are added, never renumbered. Core and agent negotiate a protocol version
  on connect.

### 3.3 Interactive terminals

A container terminal and a host terminal use the same flow. The panel asks the core for a session
(`POST /api/v1/servers/{serverID}/containers/{container}/terminal` or
`POST /api/v1/servers/{serverID}/terminal`), gets a one-time ticket and opens
`GET /api/v1/terminal/{sessionID}/ws`. The core keeps the session in the terminal manager and sends a
typed `TerminalOpen` message to the agent with `TerminalTarget` set to the container or the host. The
agent starts a PTY; output and resizing travel back and are recorded, while keystrokes are not recorded.
The panel sends the ticket first and then binary data and resize control messages.

A host terminal additionally needs `confirm` and a fresh TOTP code with the same replay guard as sign-in
and password changes, and the agent must report host-terminal support. It starts `/bin/bash -l` or
`/bin/sh -l` as root with a clean environment, is limited to 2 sessions per agent and one per user and
server, shares the 4-session per-server limit, and is audited as `host_terminal_open` and
`host_terminal_close`. It can be turned off with `[terminal] host = false` in
`/etc/talaia/agent.toml` or `TALAIA_AGENT_TERMINAL_HOST=false`.

## 4. Data model (implemented)

This is the schema created by migrations up to `0013_oidc.sql`. Stacks and containers are not
tables yet: stacks live on disk and containers come from the agent inventory. Tables for domains,
alerts, backups and metrics belong to later roadmap phases.

| Entity | Main fields |
|---|---|
| `organization` | id, name |
| `app_user` | id, username, display_name, email, password_hash, TOTP fields, failed attempts, locked_until, disabled_at, oidc_subject |
| `recovery_code` | id, user_id, code_hash, used_at |
| `session` | id, token_hash, user_id, stage (`mfa_pending` or `full`), expiry, revoked_at, IP, user agent, MFA failures |
| `trusted_device` | id, user_id, token_hash, pin_hash, name, 180-day browser marker, counters and timestamps |
| `passkey` | id, user_id, credential_id, public_key, sign counter, transports, backup flags |
| `webauthn_ceremony` | id, user_id, kind, session_data, expires_at |
| `server` | id, name, group_id, certificate serial and expiry, created_at, last_seen, revoked_at, removed_at |
| `enrollment_token` | id, token_hash, server_name, expires_at, used_at |
| `server_group` | id, name, created_at |
| `team` | id, name, created_at |
| `team_member` | team_id, user_id, created_at |
| `role_binding` | id, subject_type (`user`, `team`, `api_key`), subject_id, scope_type (`organization`, `group`, `server`, `stack`, `container`), scope_server_id, scope_group_id, scope_name, role, created_by, created_at |
| `user_invite` | id, token_hash, username, email, role, scope fields, created_by, created_at, expires_at, used_at |
| `api_key` | id, key_id, name, owner_id, secret_hash, created_at, expires_at, last_used_at, revoked_at |
| `oidc_config` | one row: enabled, issuer, client_id, client_secret_enc, button_label, updated_at, updated_by |
| `audit_event` | id, at, actor, actor_id, action, IP, result, details, scope |
| `job` | id, server_id, kind, params, state, message, output, created_by, timestamps |
| `terminal_session` | id, server_id, container or `@host`, shell, created_by, privileged, times, close_reason, recorded_bytes |
| `terminal_chunk` | session_id, seq, encrypted data |

Migration `0012_rbac.sql` also gives every existing installation user the organization administrator
binding, adds `group_id` to `server`, `display_name`/`email`/`disabled_at`/`oidc_subject` to
`app_user`, and `actor_id`/`scope` to `audit_event`. Migration `0013_oidc.sql` adds `oidc_config`
with a single row (`id boolean PRIMARY KEY DEFAULT true CHECK (id)`).

## 5. Permissions

There is one organization. **Server groups** put servers together, **teams** put users together, and a
**role binding** gives one fixed role to a user, a team or an API key at one scope.

```text
Organization → Group → Server → Stack → Container
```

A stack or container scope is identified by its server and its name. A container that belongs to a
Compose project inherits the stack's bindings; a standalone container inherits only from the server and
above. An assignment applies to its node and everything below it.

### 5.1 Roles

| Role | Permissions before scope filtering |
|---|---|
| `admin` | All permissions |
| `server_operator` | All container, stack and server permissions (including container and host terminals), but not organization permissions |
| `app_operator` | All `.view` permissions except `recordings.view`, plus `container.logs`, `container.control` and `stack.deploy` |
| `viewer` | All `.view` permissions except `recordings.view`, plus `container.logs` and `host.journal` |
| `metrics_only` | `server.view`, `stack.view`, `container.view` |

The role's list is still filtered by the minimum scope of each permission. `audit.view` and
`recordings.view` are organization-only; `host.*` and `server.agent_update` need at least a server;
`stack.edit` needs at least a stack; `container.*` can be granted on a container.

| Minimum scope | Permissions |
|---|---|
| Container | `container.view`, `container.logs`, `container.control`, `container.terminal` |
| Stack | `stack.view`, `stack.deploy`, `stack.edit`, `container.remove` |
| Server | `server.view`, `resources.manage`, `host.services.view`, `host.services.control`, `host.journal`, `host.apt.view`, `host.apt.upgrade`, `host.reboot`, `host.terminal`, `server.agent_update` |
| Organization | `server.remove`, `server.enroll`, `users.manage`, `settings.manage`, `audit.view`, `recordings.view`, `apikeys.manage` |

A permission counts only when the binding scope is at least as wide as the permission's minimum scope.
A `server_operator` bound to a stack gets container and stack permissions there, but never host access.
A group has no separate permission set; a binding there applies to its servers and their descendants.

`app_operator` deliberately lacks `stack.edit`, `container.terminal` and `host.terminal`: a Compose file
runs as root and a privileged shell reaches the host. Deploying files that already exist is allowed;
changing them, removing or forgetting the stack (both need `stack.edit`) or opening a terminal is not.
Terminal recordings hold the output of root shells, so `recordings.view` belongs to `admin` only. A `server_operator` bound to a stack gets no host endpoints,
but `stack.edit` is still root-equivalent through Compose and must be treated as such.

### 5.2 Enforcement

Every protected API route declares its permission in the single `routePerms` table in
`core/internal/api/authz.go`. The `chi.Walk` guardian test fails when a route has no declaration, so a
new endpoint cannot be accidentally public. The middleware resolves the effective bindings and the
target (organization, group, server, stack or container) and applies the rule.

- A user who cannot see the object gets **404 Not Found**; a user who can see it but lacks the
  operation gets **403 Forbidden**.
- Every denial is written to the audit log as `authz_denied`, with actor, permission, method, path,
  result and scope (and the API key id when applicable).
- List endpoints filter servers, stacks and containers by visibility. A job is visible only to its
  creator or a user with `users.manage`.
- The panel uses `GET /api/v1/me` and only hides controls; it never replaces the core check.

### 5.3 API keys

An API key has its own role bindings (`subject_type = api_key`). Its effective permissions are the
**intersection** of those bindings and its owner's current bindings, so it loses access when the owner
does. The API-key blacklist always applies: keys cannot open container or host terminals, manage
users, teams, groups, keys or settings, reboot a server, or reach sign-in endpoints. Routes marked
`noAPIKey` are rejected even when the key's bindings would otherwise allow them.

API keys authenticate with `Authorization: Bearer tlk_…` and need no cookie, CSRF token or `Origin`
header. They are limited to 600 requests per minute per key, with an additional per-IP bound; the
secret is stored hashed and shown once.

### 5.4 Last administrator

At least one active user must keep the organization administrator role, directly or through a team.
Disabling the last administrator, or removing or demoting the last organization-admin assignment (direct
or through a team), returns `409 last_admin`; a user cannot disable their own account. The check runs with
an advisory lock inside the transaction.

The full user-facing guide is [permissions.md](permissions.md).

## 6. Security summary

- Agents connect outbound; VPS do not open ports for Talaia. mTLS with per-agent certificates, revocable
  from the panel.
- Permissions are enforced in the core: five fixed roles on organization → group → server → stack →
  container. A denial is 404 when the object is hidden and 403 when it is visible but the operation is
  not allowed; every denial is audited. The panel only hides controls. See [permissions.md](permissions.md).
- Mandatory 2FA, passkeys, and OIDC for existing users only: the provider replaces the password, not the
  second factor. OIDC requires a verified email, binds the subject on first login, and creates no accounts.
- API keys are scoped, stored hashed, shown once, limited to 600 requests/minute, and limited to the
  intersection of their own bindings and the owner's current bindings. They cannot use terminals,
  user/settings administration, key management, reboot or sign-in routes.
- Invitations are single-use, expire after 72 hours, are stored hashed, and still require TOTP setup.
- Secrets (TOTP, OIDC client secret, terminal chunks) are encrypted with the secrets key; never logged.
- Audit log for every action and denial; terminal sessions are recorded. Administrators and server
  operators are root-equivalent on the servers they manage: see [security.md](security.md).
- Signed packages and images (cosign); the agent verifies signatures before self-updating.
- Recommended deployment: panel reachable only through WireGuard or Tailscale.

### 6.1 Panel authentication and authorization (implemented)

The first administrator is created on the server console (`talaia-core user create --username <name>`);
there is never a public sign-up page. Further users arrive through an invitation and must set up TOTP.

- Password (12–256 characters) hashed with argon2id; mandatory TOTP from the first sign-in, a code can
  never be used twice, and the TOTP secret is encrypted with AES-256-GCM (`data_dir/secrets.key`).
- 10 single-use recovery codes, shown once. Console recovery: `talaia-core user reset-2fa` and
  `talaia-core session revoke-all`.
- Session cookie `__Host-talaia_session` (`Secure`, `HttpOnly`, `SameSite=Strict`), stored hashed; 12 h idle
  and 7 days maximum; a new token after the second factor.
- CSRF: JSON body and an allowed `Origin` for every state-changing request. Security headers (CSP, HSTS,
  `nosniff`, `no-referrer`).
- OIDC uses discovery, authorization code with PKCE, `state` and `nonce` in a sealed 10-minute cookie,
  and verifies the ID token. Failures redirect to the generic login error and are audited; they never
  reveal whether an account exists.
- API keys use `Authorization: Bearer tlk_…` with no cookie, CSRF token or `Origin`; they are rejected on
  the routes marked `noAPIKey` even when their role bindings would allow them.
- Brute force: 10 attempts per IP every 15 minutes, 5 failures lock the account for 15 minutes; errors never
  say what was wrong. Every sign-in, failure, denial and change goes to the audit log, without secrets.
- At least one active organization administrator must remain; the store refuses to disable the last
  administrator or remove or demote the last organization-admin assignment (`409 last_admin`).

## 7. File locations on managed servers

| Path | Content |
|---|---|
| `/etc/talaia/agent.toml` | Agent configuration |
| `/etc/talaia/certs/` | Agent key and certificate (mode 0600) |
| `/var/lib/talaia/` | Local buffer, state |
| `/opt/talaia/stacks/<name>/` | `compose.yaml` and `.env` of each stack |

## 8. Whole-server operations (phase 3)

Phase 3 is implemented and was closed in 0.4.0 after the real test on two servers and its security
review. It covers the server screen, apt status and upgrades, systemd services, journald reads, the host
terminal and the audit screen. The user guide is [server.md](server.md).

### 8.1 Typed commands and job kinds

The core never sends a shell line. Each operation is a typed `CoreMsg` with a `job_id`; the agent
checks the message again and answers with `JobEvent` messages.

| Job kind | Command | Notes |
|---|---|---|
| `apt_status` | `AptStatus{refresh:false}` | Reads the cached apt summary. |
| `apt_refresh` | `AptStatus{refresh:true}` | Runs `apt-get update` first; longer timeout. |
| `apt_upgrade` | `AptUpgrade{mode}` | `security` or `all`; the agent computes the package list. |
| `host_reboot` | `HostReboot{hostname}` | Schedules `systemctl reboot` five seconds later. |
| `service_action` | `ServiceAction{unit,action}` | Start, stop, restart, reload, enable or disable. |
| `journal_read` | `ReadJournal{...}` | Journal entries and cursor in the job output. |

The terminal is not a job: the manager keeps the live session and the browser attaches over a WebSocket.

The protocol additions are the `ServiceAction`, `ReadJournal`, `AptStatus`, `AptUpgrade` and `HostReboot`
commands, `AptSummary` and `host_terminal` in `Inventory`, `unit_file_state` in `SystemdUnit`, and
`TerminalOpen.target` with `TerminalTarget` (`UNSPECIFIED` remains the container for compatibility).

### 8.2 Endpoints

All routes are under `/api/v1` and require a full session. The container and stack routes of phase 2
are unchanged.

| Method and path | Body or query | Job | Audit action |
|---|---|---|---|
| `GET /servers/{serverID}/services` | - | - | none (read) |
| `POST /servers/{serverID}/services/{unit}/actions` | `{action,confirm}` | `service_action` | `service_<action>` |
| `POST /servers/{serverID}/apt/status` | `{refresh}` (optional) | `apt_status` or `apt_refresh` | `apt_status` |
| `POST /servers/{serverID}/apt/upgrade` | `{mode:"security" or "all",confirm:true}` | `apt_upgrade` | `apt_upgrade` |
| `POST /servers/{serverID}/reboot` | `{confirm:"<hostname>"}` | `host_reboot` | `host_reboot` |
| `POST /servers/{serverID}/journal` | filters (see below) | `journal_read` | `journal_read` |
| `POST /servers/{serverID}/terminal` | `{confirm:true,code,shell,cols,rows}` | terminal session | `host_terminal_open` / `host_terminal_close` |
| `GET /terminal/{sessionID}/ws` | WebSocket, ticket first | - | - |
| `GET /audit` | `limit,before,action,server_id,actor` | - | none (read) |
| `GET /jobs/{jobID}` | - | - | - |

Journal body: `unit`, `max_priority` (0-7), `since` and `until` (RFC 3339), `search` (literal,
at most 200 characters), `boot` (`all`, `current`, `previous`), `tail` (1-1000, 200 by default)
and `after_cursor` for following. The audit entry stores the unit and boot, not the search or the lines.

Errors use the same codes as the rest of the API: `invalid_request`, `confirmation_required`,
`agent_offline`, `operation_in_progress`, `self_management`, `too_many_terminals`,
`terminal_exists`, `code_invalid`, `host_terminal_disabled`, `agent_busy` and
`terminal_open_failed`.

### 8.3 apt in a transient systemd unit

The agent computes the package list for a security upgrade; the core never sends names. The upgrade runs
through `systemd-run` in a unit named `talaia-apt-<short job id>`:

```text
systemd-run --unit=talaia-apt-<id> --collect --property=Type=exec
  --property=RemainAfterExit=yes
  --property=StandardOutput=append:/var/lib/talaia/apt/<job>.log
  --setenv=DEBIAN_FRONTEND=noninteractive --setenv=LC_ALL=C
  -- apt-get -y -o Dpkg::Options::=--force-confdef
     -o Dpkg::Options::=--force-confold -o DPkg::Lock::Timeout=120
     --no-remove upgrade --with-new-pkgs
```

`security` uses `install --only-upgrade -- <packages>` instead. The agent writes
`/var/lib/talaia/apt/<job>.json` and polls `systemctl show`; it emits `RUNNING` with a log tail
every few seconds and resumes the unit after an agent restart. Only one apt upgrade or reboot runs at a
time. State files are cleaned after 24 hours and logs after 30 days.

### 8.4 Services and journal data flow

For a service action the core checks that the unit was in the last inventory, that `confirm` is present
when the action needs it, and that `talaia-agent.service` is not being stopped, restarted or disabled.
It creates a `service_action` job and sends the typed `ServiceAction`; the agent runs
`systemctl --no-ask-password` with a fixed verb, returns the new state and refreshes the inventory.

For a journal read the core validates every filter, creates a `journal_read` job and sends
`ReadJournal`. The agent runs `journalctl --output=json --no-pager` with separate arguments, caps each
message at 4 KiB and the total output at 256 KiB, and returns `{entries,cursor}`. The panel follows the
cursor every 3 seconds.

### 8.5 Host terminal flow

1. The panel asks `POST /servers/{serverID}/terminal` with `confirm`, a fresh TOTP `code`, the
   shell and the size.
2. The core checks the full session, the agent connection and `host_terminal` capability, applies the
   shared rate limiter, verifies the TOTP code with its replay guard and asks the terminal manager for a
   session.
3. The manager reserves the limits, stores the session (container `@host`), creates the recorder and
   sends the typed `TerminalOpen` with `TerminalTarget.HOST`.
4. The agent starts a login shell on a local PTY; the manager audits `host_terminal_open`, issues a
   30-second ticket and answers 201.
5. The browser opens the WebSocket, sends the ticket and the session becomes live. Output and resizing are
   recorded and encrypted; keystrokes are not recorded. Closing audits `host_terminal_close`.

### 8.6 Data on the managed server

| Path | Content |
|---|---|
| `/var/lib/talaia/apt/<job>.json` | Apt job state for resuming after an agent restart. |
| `/var/lib/talaia/apt/<job>.log` | Apt output, kept for 30 days. |
| `talaia-apt-<id>` | Transient systemd unit of one apt upgrade. |
| `talaia-reboot` | Transient unit that runs `systemctl reboot`. |
