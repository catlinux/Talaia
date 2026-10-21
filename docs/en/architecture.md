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

Go CLI that uses the same REST API with an API key. Intended for scripts, CI pipelines and GitOps sync.

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

## 4. Data model (initial)

| Entity | Main fields |
|---|---|
| `organization` | id, name |
| `user` | id, email, password_hash, totp_secret, locale |
| `passkey` | id, user_id, credential |
| `team` | id, name |
| `team_member` | team_id, user_id |
| `server_group` | id, name, tags |
| `server` | id, group_id, name, os, arch, agent_version, cert_fingerprint, last_seen, status |
| `stack` | id, server_id, name, source (git/local), compose, env_encrypted, current_revision |
| `stack_revision` | id, stack_id, compose, author_id, created_at |
| `container` | id, server_id, stack_id (nullable), docker_id, image, state — inventory cache |
| `domain` | id, stack_id, host, port, tls_status |
| `role_binding` | subject (user/team), scope_type, scope_id, role |
| `job` | id, server_id, kind, payload, status, output, created_by |
| `alert_rule` | id, scope, metric, threshold, channels |
| `backup_plan` | id, target, schedule, destination, retention |
| `audit_event` | id, actor, action, scope, ip, result, created_at |
| `metric` | TimescaleDB hypertable: server_id, container_id, ts, values |

## 5. Permissions

Scopes form a tree: **organization → group → server → stack → container**. A role granted on a node
applies to everything below it.

| Role | Permissions |
|---|---|
| Admin | Everything, including users and settings |
| Server operator | Everything on its servers: containers, services, packages, terminal |
| App operator | View, logs, restart, deploy on its stacks; no terminal, no host access |
| Viewer | Read only |
| Metrics only | Metrics and status, no logs |

Permissions are always checked in the core. The web panel only hides what the user cannot use.

## 6. Security summary

- Agents connect outbound; VPS do not open ports for Talaia.
- mTLS with per-agent certificates; certificates can be revoked from the panel.
- Mandatory 2FA; passkeys and OIDC supported.
- Secrets encrypted with `age`; never logged.
- Audit log for every action; terminal sessions recorded. The administrator is root on the managed servers: see [security.md](security.md).
- Signed packages and images (cosign); the agent verifies signatures before self-updating.
- Recommended deployment: panel reachable only through WireGuard or Tailscale.

### 6.1 Panel authentication (implemented)

Single administrator created on the server console (`talaia-core user create --username <name>`); there is
never a public sign-up page. Design: `.agents/plans/auth/auth.DESIGN.md`.

- Password (12–256 characters) hashed with argon2id; mandatory TOTP from the first sign-in, a code can
  never be used twice, and the TOTP secret is encrypted with AES-256-GCM (`data_dir/secrets.key`).
- 10 single-use recovery codes, shown once. Console recovery: `talaia-core user reset-2fa` and
  `talaia-core session revoke-all`.
- Session cookie `__Host-talaia_session` (`Secure`, `HttpOnly`, `SameSite=Strict`), stored hashed; 12 h idle
  and 7 days maximum; a new token after the second factor.
- CSRF: JSON body and an allowed `Origin` for every state-changing request. Security headers (CSP, HSTS,
  `nosniff`, `no-referrer`).
- Brute force: 10 attempts per IP every 15 minutes, 5 failures lock the account for 15 minutes; errors never
  say what was wrong. Every sign-in, failure and change goes to the audit log, without secrets.

## 7. File locations on managed servers

| Path | Content |
|---|---|
| `/etc/talaia/agent.toml` | Agent configuration |
| `/etc/talaia/certs/` | Agent key and certificate (mode 0600) |
| `/var/lib/talaia/` | Local buffer, state |
| `/opt/talaia/stacks/<name>/` | `compose.yaml` and `.env` of each stack |
