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
  <img alt="Version" src="https://img.shields.io/badge/version-0.0.1-0B7285">
  <img alt="Status" src="https://img.shields.io/badge/status-design%20phase-B86E00">
  <img alt="License" src="https://img.shields.io/badge/license-WNCL--1.0-56666F">
  <img alt="Languages" src="https://img.shields.io/badge/i18n-ca%20%C2%B7%20es%20%C2%B7%20en-0B7285">
</p>

> [!WARNING]
> Talaia is in its **design phase**. There is no usable code yet. This repository currently holds the
> project definition, roadmap and mockups. Follow the [roadmap](ROADMAP.md) to see what comes next.

---

## Table of contents

- [What is Talaia?](#what-is-talaia)
- [Why another panel?](#why-another-panel)
- [Two levels of management](#two-levels-of-management)
- [Planned features](#planned-features)
- [Architecture](#architecture)
- [Supported systems](#supported-systems)
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
- **`talaiactl`**: a command line tool that does everything the panel does.

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

You can give a person access to **a single app** without them ever seeing the server it runs on.

**No lock-in.** Stacks live on disk at `/opt/talaia/stacks/<name>/compose.yaml`. If you uninstall
Talaia, everything keeps running and you can manage it with `docker compose` as usual.

## Planned features

Each feature is tagged with the release where it is planned. See [ROADMAP.md](ROADMAP.md) for detail.

| Area | MVP (0.4.0) | v1 (1.0.0) | Later |
|---|---|---|---|
| Containers and stacks | Actions, live logs, Compose editor, container terminal, images/volumes/networks, adopt existing | Revision history and rollback, image update notices | Podman |
| Whole server | Metrics, systemd, journald, apt updates, host terminal | Firewall, fail2ban, users and SSH keys, cron, file browser, hardening audit | — |
| Deploys and apps | — | Stacks from Git, Dockerfile/Nixpacks builds, zero-downtime deploys, domains + SSL (Caddy), template catalog | Preview environments per pull request |
| Monitoring and alerts | — | Metrics history, alert rules, HTTP/TCP checks, certificate expiry, Telegram/ntfy/email/Discord/webhook | Public status page |
| Backups | — | restic plans, consistent database dumps, S3/B2/SFTP, restore from the panel | Automatic monthly restore test |
| Fleet automation | — | Bulk actions, scheduled procedures, GitOps sync | Ansible playbooks, VPS provisioning via provider APIs |
| Users and security | Single user with 2FA, audit log | Users, teams, scoped roles, passkeys, OIDC SSO, terminal session recording, scoped API keys | — |

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

Full detail: [docs/en/architecture.md](docs/en/architecture.md).

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

## Technology stack

| Layer | Choice |
|---|---|
| Agent | Go · Docker SDK · go-systemd (D-Bus) · sdjournal · gopsutil · creack/pty |
| Core | Go · chi · connect-go (gRPC) · sqlc · River (job queue) |
| Database | PostgreSQL 16 + TimescaleDB *(to be confirmed in phase 0)* |
| Web panel | React · TypeScript · Vite · TanStack Query/Router · Tailwind · xterm.js · Monaco · uPlot *(to be confirmed in phase 0)* |
| i18n | i18next (web) · go-i18n (core, agent, CLI) |
| Reverse proxy | Caddy (managed through its admin API) |
| Backups | restic |
| Auth | Sessions + TOTP · WebAuthn passkeys · OIDC |
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

Project **documentation** is written in **English and Spanish**.

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

Licensed under [WNCL-1.0](LICENSE): free to use, study, modify and share for non-commercial purposes. Selling or any commercial use requires written permission.

Copies and forks must keep this attribution and link to https://github.com/catlinux/Talaia.

### Third-party material

This project contains no third-party material.
