<h1 align="center">Talaia</h1>

<p align="center">
  <b>Self-hosted control panel for Debian VPS fleets.</b><br>
  Manage a single Docker container or an entire server, from one place.
</p>

<p align="center">
  <a href="README.es.md">Leer en castellano</a> ·
  <a href="ROADMAP.md">Roadmap</a> ·
  <a href="TODO.md">TODO</a> ·
  <a href="CHANGELOG.md">Changelog</a> ·
  <a href="LICENSE">License</a>
</p>

<p align="center">
  <img alt="Version" src="https://img.shields.io/badge/version-0.4.14-0B7285">
  <img alt="Status" src="https://img.shields.io/badge/status-phase%204%20in%20progress-B86E00">
  <img alt="License" src="https://img.shields.io/badge/license-WNCL--TP--1.0-56666F">
  <img alt="Languages" src="https://img.shields.io/badge/i18n-ca%20%C2%B7%20es%20%C2%B7%20en-0B7285">
</p>

> [!WARNING]
> Talaia is in active development. Phase 3 (whole server) closed in 0.4.0. Phase 4 (users, permissions,
> API keys, OIDC, recordings and talaiactl) is implemented, but it is not closed yet: it still needs the
> security review and the real test with a second user. Follow the [roadmap](ROADMAP.md) for what comes next.

---

## Table of contents

- [What is Talaia?](#what-is-talaia)
- [Why another panel?](#why-another-panel)
- [Two levels of management](#two-levels-of-management)
- [Users and permissions](#users-and-permissions)
- [Planned features](#planned-features)
- [Architecture](#architecture)
- [Supported systems](#supported-systems)
- [Installation](#installation)
- [Sign-in and access](#sign-in-and-access)
- [Technology stack](#technology-stack)
- [Languages (i18n)](#languages-i18n)
- [Repository layout](#repository-layout)
- [Mockups](#mockups)
- [Versioning](#versioning)
- [Development workflow](#development-workflow)
- [Contributing](#contributing)
- [Security](#security)
- [Credits and license](#credits-and-license)

---

## What is Talaia?

A *talaia* is the Catalan word for a coastal watchtower: from a single point you can see the whole
coastline and raise the alarm when something goes wrong. That is exactly what this panel does for your
servers.

Talaia is a **self-hosted web panel** to manage a fleet of Debian-based VPS. It is made of:

- **`talaia-core`**: a central server (API, database, task queue, alerts) that you host yourself.
- **`talaia-agent`**: a small binary installed on every VPS. It connects *outbound* to the core, so your
  servers do not need any open port for Talaia.
- **`talaia-web`**: the web panel, available in **Catalan, Spanish and English**.
- **`talaiactl`**: a Go command-line client for scripts and CI. It signs in with a scoped API key and
  exposes servers, containers, stacks and jobs.

## Why another panel?

Every existing tool solves part of the problem. Talaia takes the best idea from each one:

| Tool | What Talaia takes from it | What Talaia improves |
|---|---|---|
| Portainer | Visual container management, teams and permissions, app templates | Fine-grained permissions and multi-server are included, not paid extras |
| Komodo | One agent per server, stacks from Git, declarative sync (GitOps) | Friendlier daily UI, plus full host management |
| Coolify / Dokploy | Deploy from Git, domains with automatic SSL, database backups | Results are plain Compose stacks, no proprietary format |
| Dockge | `compose.yaml` on disk is the source of truth | Same principle, plus revision history |
| Cockpit / Webmin | Whole-server management: systemd, journald, packages, firewall, terminal | Built for many servers at once |
| Beszel / Netdata | Lightweight per-server and per-container metrics | Integrated with alerts and the fleet view |
| Uptime Kuma | HTTP/TCP checks and notifications | Linked automatically to every app and domain |
| Ansible / Semaphore | Run the same task across many servers | Predefined actions, no YAML required |
| Teleport | Audited web terminal with session recording | Built in, no extra infrastructure |

## Two levels of management

This is the core requirement of the project. Talaia works at two levels on the same data model:

**Container mode: one app, isolated**

- Compose stacks and standalone containers
- Edit `compose.yaml` and environment variables, with revision history
- Logs, stats and a terminal inside the container
- Deploy, restart and roll back to a previous revision
- Domain and SSL, volume backups, deploy webhooks
- Adopt containers and Compose projects created outside Talaia

**Server mode: the whole machine**

- System metrics, disks and network
- systemd services and journald logs
- apt packages, security updates and "reboot required" notices
- Firewall (nftables/ufw) and fail2ban
- Users, SSH keys, cron jobs and timers
- File browser, host terminal and hardening audit

Permissions follow the same hierarchy and are inherited downwards:

```
Organization → Group (prod, clients…) → Server → Stack / App → Container
```

You can give a person access to **a single app** without them ever seeing the server it runs on. The
roles and scope rules are in [docs/en/permissions.md](docs/en/permissions.md).

**No lock-in.** Stacks live on disk at `/opt/talaia/stacks/<name>/compose.yaml`. If you uninstall
Talaia, everything keeps running and you can manage it with `docker compose` as usual.

## Users and permissions

Phase 4 adds one organization with server groups, teams and five fixed roles: **administrator, server
operator, app operator, viewer and metrics only**. Assignments can be made at organization, group, server,
stack or container scope and are inherited downwards. An app operator can deploy an existing stack and read
its logs, but cannot edit its Compose file or open a terminal, because both are equivalent to root on the
server.

The same phase adds invitations by link, scoped API keys, OpenID Connect sign-in for existing users (the
second factor stays mandatory), terminal recording downloads and `talaiactl`. Full guide:
[docs/en/permissions.md](docs/en/permissions.md).

## Planned features

Each feature is tagged with the release where it is planned. See [ROADMAP.md](ROADMAP.md) for detail.

| Area | MVP (0.4.0) | v1 (1.0.0) | Later |
|---|---|---|---|
| Containers and stacks | Actions, live logs, Compose editor, container terminal, images/volumes/networks, adopt existing | Revision history and rollback, image update notices | Podman |
| Whole server | Metrics, systemd, journald, apt updates and reboot, host terminal (phase 3 closed in 0.4.0) | Firewall, fail2ban, users and SSH keys, cron, file browser, hardening audit | — |
| Deploys and apps | — | Stacks from Git, Dockerfile/Nixpacks builds, zero-downtime deploys, domains + SSL (Caddy), template catalog | Preview environments per pull request |
| Monitoring and alerts | — | Metrics history, alert rules, HTTP/TCP checks, certificate expiry, Telegram/ntfy/email/Discord/webhook | Public status page |
| Backups | — | restic plans, consistent database dumps, S3/B2/SFTP, restore from the panel | Automatic monthly restore test |
| Fleet automation | — | Bulk actions, scheduled procedures, GitOps sync | Ansible playbooks, VPS provisioning via provider APIs |
| Users and security | Single user with 2FA, trusted-device PIN, passkeys, audit log and recorded terminals (0.4.0) | Users, teams, server groups, scoped roles, invitations, API keys, OIDC SSO and talaiactl (implemented for 0.5.0; phase not closed) | — |

> **Phase 3 status.** Closed in 0.4.0 after a real test on two servers and its security review. The guide
> is [docs/en/server.md](docs/en/server.md).

## Architecture

```
 Browser / talaiactl / Git webhooks
              │  HTTPS (REST + WebSocket)
              ▼
 ┌───────────────────────────────┐
 │          talaia-core          │   PostgreSQL 16 + TimescaleDB
 │ API · auth · RBAC · audit     │
 │ agent hub · jobs · alerts     │
 │ deploys · secrets (age)       │
 └───────────────────────────────┘
              ▲  gRPC over mTLS, port 443
              │  (connection opened by the agent)
   ┌──────────┼──────────────┐
   │          │              │
 agent     agent           agent        ← one per VPS (Docker, systemd,
 vps-web   vps-db          rpi-home        journald, apt, metrics, PTY…)
```

Key decisions:

- **Outbound-only agents.** VPS behind NAT or a strict firewall work without changes.
- **Typed commands.** The core never sends arbitrary shell. It sends typed, idempotent jobs, plus explicit
  interactive terminals that are recorded in the audit log.
- **Mutual TLS.** Each agent gets its own certificate from an internal CA during enrollment (90-day
  validity, automatic renewal).
- **Graceful degradation.** If the core goes down, containers and services keep running. The agent keeps
  24 h of metrics and events locally and sends them when it reconnects.

Full detail: [docs/en/architecture.md](docs/en/architecture.md). Users, roles and API keys:
[docs/en/permissions.md](docs/en/permissions.md). What the panel can do to your servers, and what is
recorded: [docs/en/security.md](docs/en/security.md).

## Supported systems

| System | Agent | Core | Notes |
|---|---|---|---|
| Debian 12 / 13 | MVP | MVP | Primary platform. Everything is tested here first. |
| Ubuntu 22.04 / 24.04 LTS | MVP | MVP | Same apt and systemd stack. |
| Raspberry Pi OS (arm64) | v1 | v1 | Go cross-compilation. |
| Rocky / Alma / Fedora | Later | v1 | Needs dnf and firewalld modules. |
| Alpine | Later | v1 | No systemd (OpenRC). |
| Architectures | amd64, arm64 | amd64, arm64 | |
| Containers | Docker Engine 24+ with Compose v2 | | Podman later. |
| Browsers | Recent Chrome, Firefox, Safari, Edge | | Responsive panel for quick actions on mobile. |

## Installation

The core runs in Docker on one machine; each server gets an agent installed with one pasted command.
Step by step, with troubleshooting: [docs/en/installation.md](docs/en/installation.md). The whole-server
screen is documented in [docs/en/server.md](docs/en/server.md).

On a fresh Debian or Ubuntu, the one-line bootstrap clones the current `main` into `/opt/talaia` and runs the
installer (before 1.0 this installs `main`, not a tagged release):

```bash
curl -fsSL https://raw.githubusercontent.com/catlinux/Talaia/main/install-core.sh | bash
```

Pass installer flags after `bash -s --`; for example, a public name and a trusted certificate with two DNS records
(the installer prints them and checks them; no API key):

```bash
curl -fsSL https://raw.githubusercontent.com/catlinux/Talaia/main/install-core.sh | bash -s -- \
  --hostname talaia.example.com --acme delegation
```

The bootstrap uses `TALAIA_DIR` (default `/opt/talaia`), `TALAIA_REPO_URL` (default
`https://github.com/catlinux/Talaia.git`) and `TALAIA_BRANCH` (default `main`). The installer accepts
`--acme-zone <zone>` (the registered domain; default: the last two labels of the host name) and
`--no-acme-check` (skip the OVH key check for an offline install). Certificate guides:
[docs/en/dns-delegation.md](docs/en/dns-delegation.md) (two DNS records, recommended) and
[docs/en/ovh-dns.md](docs/en/ovh-dns.md) (OVH API key).

You can also clone it and run the installer yourself:

```bash
git clone https://github.com/catlinux/Talaia.git && cd Talaia
packaging/core/install.sh --hostname <address> --port 8443 --agent-port 8444
```

## Sign-in and access

Password and TOTP are the base. A browser can also become a **trusted device** and open the panel with a PIN,
and **passkeys** can sign in with the fingerprint, the face or the device PIN. Both are optional and keep the
password, TOTP and recovery codes as the way back in.

An administrator can also enable OpenID Connect. It replaces the password only: the second factor is
still required, and users must already exist with the same verified email.

How to set up, use, remove and troubleshoot each method: [docs/en/access.md](docs/en/access.md). Users,
roles, invitations and API keys are in [docs/en/permissions.md](docs/en/permissions.md). The security model
and what is recorded are in [docs/en/security.md](docs/en/security.md).

## Technology stack

| Layer | Choice |
|---|---|
| Agent | Go 1.26 · Docker SDK · go-systemd (D-Bus) · sdjournal · gopsutil · creack/pty |
| Core | Go · chi · connect-go (gRPC) · sqlc · River (job queue) |
| Database | PostgreSQL 16 + TimescaleDB |
| Web panel | React · TypeScript · Vite · TanStack Query/Router · Tailwind · xterm.js · Monaco · uPlot |
| i18n | i18next (web) · go-i18n (core, agent, CLI) |
| Reverse proxy | Caddy (managed through its admin API) |
| Backups | restic |
| Auth | Password + TOTP · trusted devices with PIN · WebAuthn passkeys · recovery codes · OIDC |
| Packaging | GoReleaser · nfpm (.deb) · cosign |
| Tests | go test · testcontainers · Playwright · Debian VMs |

## Languages (i18n)

Talaia is multilingual from day one. Supported languages:

| Code | Language | Status |
|---|---|---|
| `ca` | Català | Reference language for the UI |
| `es` | Castellano | Required |
| `en` | English | Required, fallback language |

Every user-facing string goes through the i18n system. A change that adds text without all three
translations is not accepted. The language is taken from the user's profile, then the browser, then
falls back to English. Detail: [docs/en/i18n.md](docs/en/i18n.md).

Project **documentation** is written in **English and Spanish**. Some guides, such as the sign-in guide, are
also available in **Catalan**.

## Repository layout

The layout below is the target structure. Folders are created as each phase starts.

```
Talaia/
├─ core/            # talaia-core: API, agent hub, jobs, alerts
├─ agent/           # talaia-agent and its modules
├─ proto/           # shared gRPC contract
├─ web/             # web panel (React)
├─ cli/             # talaiactl
├─ templates/       # app catalog (compose + metadata)
├─ packaging/       # .deb, systemd units, install.sh, core compose.yaml
├─ scripts/         # maintenance scripts (license headers, releases…)
├─ test/e2e/        # end-to-end tests on Debian VMs
└─ docs/
   ├─ en/           # English documentation
   ├─ es/           # Spanish documentation
   └─ design/       # roadmap page and mockups (HTML)
```

## Mockups

Open [`docs/design/talaia-roadmap.html`](docs/design/talaia-roadmap.html) in a browser. It contains the
full project proposal and five mockups: fleet view, whole-server view, stack view, new-app wizard and
scoped permissions.

## Versioning

Talaia uses [Semantic Versioning](https://semver.org/) `MAJOR.MINOR.PATCH`. The current version is in
the [`VERSION`](VERSION) file.

While in development (`0.y.z`), each completed roadmap phase raises the minor version:

| Version | Milestone |
|---|---|
| 0.0.x | Project definition and documentation |
| 0.1.0 | Phase 0: foundations |
| 0.2.0 | Phase 1: agent, connection and inventory |
| 0.3.0 | Phase 2: containers and stacks |
| **0.4.0** | **Phase 3: whole server — MVP** |
| 0.5.0 – 0.9.0 | Phases 4 to 8 |
| **1.0.0** | **Phase 9: first stable release** |

Patch versions (`z`) are used for fixes and small changes inside a phase.

## Development workflow

```
Windows (VS Code + Claude Code)  ──commit──▶  local repo
                                  ──push (approved by maintainer)──▶  GitHub
Linux test machine               ◀──clone / pull──  GitHub  ──▶  build and test
```

- Day-to-day work happens on a Windows machine with VS Code and Claude Code.
- Claude Code acts as **orchestrator**; other AI agents act as **executors** (coordinated with Agent Relay).
  Rules for agents: [CLAUDE.md](CLAUDE.md) and [AGENTS.md](AGENTS.md).
- Commits can be made freely. **Pushes are always approved by the maintainer.**
- Changes are tested on a Linux machine that clones the repository from GitHub.

Detail: [docs/en/development.md](docs/en/development.md).

## Contributing

Contributions are welcome under the terms of the license. Read [CONTRIBUTING.md](CONTRIBUTING.md) before
opening an issue or pull request.

## Security

Do not report vulnerabilities in public issues. See [SECURITY.md](SECURITY.md).

## Credits and license

**Talaia** is developed and maintained by **CatLinux**.
Copyright (C) 2026 CatLinux.

Licensed under [WNCL-TP-1.0](LICENSE): free to use, study, modify and share for non-commercial purposes. Selling or any commercial use requires written permission. It covers only original material; third-party components keep their own licenses.

Copies and forks must keep this attribution and link to https://github.com/catlinux/Talaia.

### Third-party material

| Component | Author | License | Link |
| --- | --- | --- | --- |
| Familjen Grotesk (font) | The Familjen Grotesk Project Authors | OFL-1.1 | https://github.com/Familjen-Sthlm/Familjen-Grotesk |
| IBM Plex Sans (font) | IBM Corp. | OFL-1.1 | https://github.com/IBM/plex |
| JetBrains Mono (font) | The JetBrains Mono Project Authors | OFL-1.1 | https://github.com/JetBrains/JetBrainsMono |
| 41 Go modules linked into the binaries | Their respective authors | MIT, BSD-3-Clause, ISC, Apache-2.0 | [CREDITS.md](CREDITS.md) |
| 40 npm packages bundled into the web panel | Their respective authors | MIT, ISC | [CREDITS.md](CREDITS.md) |

The license text and copyright notice of each component is in [THIRD-PARTY-NOTICES.txt](THIRD-PARTY-NOTICES.txt).

Full credits: [CREDITS.md](CREDITS.md).
