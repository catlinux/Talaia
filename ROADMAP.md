# Talaia roadmap

[Leer en castellano](ROADMAP.es.md) · [Back to README](README.md) · [Visual version with mockups](docs/design/talaia-roadmap.html)

This roadmap splits the project into **10 phases**. Each phase ends with something usable on real servers
and has a concrete **"done when"** criterion. A phase does not start until the previous one meets its
criterion.

Estimates assume **one person working 15–20 hours per week**. Total: about **38 weeks**.

```
Week   0    2        6          11         16      19      22           28      31        35      38
       ├─F0─┼───F1───┼────F2────┼────F3────┼───F4──┼───F5──┼─────F6─────┼───F7──┼────F8───┼───F9──┤
       0.1.0  0.2.0     0.3.0      0.4.0 MVP  0.5.0   0.6.0    0.7.0      0.8.0    0.9.0    1.0.0
```

| Phase | Name | Weeks | Version | Status |
|---|---|---|---|---|
| — | Project definition and documentation | — | 0.0.x | 🟢 Done |
| 0 | Foundations | 1–2 | 0.1.0 | 🟢 Done |
| 1 | Agent, connection and inventory | 3–6 | 0.2.0 | 🟢 Done |
| 2 | Containers and stacks | 7–11 | 0.3.0 | 🟢 Done |
| 3 | Whole server (**MVP**) | 12–16 | 0.4.0 | 🟢 Done |
| 4 | Users, permissions and audit | 17–19 | 0.5.0 | 🟡 In progress |
| 5 | Monitoring and alerts | 20–22 | 0.6.0 | ⚪ Pending |
| 6 | Deploys, domains and templates | 23–28 | 0.7.0 | ⚪ Pending |
| 7 | Backups | 29–31 | 0.8.0 | ⚪ Pending |
| 8 | Fleet and automation | 32–35 | 0.9.0 | ⚪ Pending |
| 9 | Version 1.0 | 36–38 | 1.0.0 | ⚪ Pending |

Legend: ⚪ pending · 🟡 in progress · 🟢 done

---

## Phase 0 · Foundations (weeks 1–2) → 0.1.0

Prepare the ground so that everything else goes fast.

- Monorepo structure, CI with tests and lint (GitHub Actions)
- `proto/` contract v0 and code generation
- Core skeleton with PostgreSQL and migrations
- Web panel skeleton, design tokens, light/dark theme
- **i18n set up from the start**: i18next in the web panel, go-i18n in Go, `ca`/`es`/`en` locale files
  and a CI check that fails if a key is missing in any language
- Maintainer's WNCL-TP-1.0 license script integrated in CI
- Two disposable test VPS (Debian 12 and Debian 13)
- Open decisions closed: PostgreSQL + TimescaleDB, React, Go 1.26, Node.js 24 LTS + pnpm

**Done when:** `make dev` starts core + panel + an empty agent locally, and CI is green.

## Phase 1 · Agent, connection and inventory (weeks 3–6) → 0.2.0

The heart of the system. If this is solid, the rest is adding modules.

- Enrollment with a one-time token, internal CA, mTLS
- gRPC tunnel with reconnection, exponential backoff and heartbeat
- Inventory: OS, hardware, containers, Compose projects, services
- Live metrics (CPU, RAM, disk, network)
- Fleet screen (mockup 1)
- `.deb` package, systemd unit and `install.sh`

**Done when:** your real VPS show up in the fleet with metrics and survive a core restart.

## Phase 2 · Containers and stacks (weeks 7–11) → 0.3.0

The replacement for Portainer and Dockge.

- Containers: start, stop, restart, remove, logs, stats, terminal
- Compose stacks stored in `/opt/talaia/stacks/<name>/`
- `compose.yaml` and `.env` editor with validation
- Adopt existing containers and Compose projects
- Images, volumes, networks and cleanup
- Stack screen (mockup 3)

**Done when:** you can stop using Portainer for everything you do today.

## Phase 3 · Whole server (weeks 12–16) → 0.4.0 · MVP

The replacement for Cockpit, across many servers.

- systemd: status, start/stop, enable/disable
- journald with filters and search
- apt: pending updates, security updates, upgrade, "reboot required"
- Host terminal
- Single user with 2FA (TOTP) and audit log
- Server screen (mockup 2)

**Done when:** you go a whole week without logging into your VPS over SSH.

> **MVP milestone (~4 months).** Talaia is now your daily tool. From here on, each phase adds power.

## Phase 4 · Users, permissions and audit (weeks 17–19) → 0.5.0

Open the tool to other people safely.

- Users, teams, roles and scopes (organization → group → server → stack → container)
- Passkeys (WebAuthn) and OIDC SSO
- Terminal session recording
- Scoped API keys and first version of `talaiactl`
- Permissions screen (mockup 5)

**Done when:** a user with access to a single stack cannot see or touch anything else, verified by
automated tests.

## Phase 5 · Monitoring and alerts (weeks 20–22) → 0.6.0

Talaia warns you before your users do.

- Metrics history in TimescaleDB with downsampling
- Alert rules and silencing
- HTTP/TCP checks and certificate expiry
- Notifications: Telegram, ntfy, email, Discord, webhook

**Done when:** if a container stops or a disk goes over 90%, you are notified in under a minute.

## Phase 6 · Deploys, domains and templates (weeks 23–28) → 0.7.0

The Coolify part.

- Stacks from Git and push webhooks
- Builds with Dockerfile and Nixpacks
- Zero-downtime deploys and rollback
- Managed Caddy: domains + automatic SSL
- Template catalog (15 initial apps)
- New app wizard (mockup 4)

**Done when:** pushing to `main` publishes the app over HTTPS with nothing else to do.

## Phase 7 · Backups (weeks 29–31) → 0.8.0

The feature you hope you never need, but that has to work.

- restic plans for volumes and folders
- Consistent database dumps (PostgreSQL, MariaDB)
- Destinations: S3, Backblaze B2, SFTP
- Restore from the panel

**Done when:** you have restored a real database onto a new server from the panel.

## Phase 8 · Fleet and automation (weeks 32–35) → 0.9.0

The Komodo and Ansible part.

- Bulk actions per group
- Scheduled procedures (chained steps)
- Firewall, SSH users, cron, file browser
- Hardening audit
- GitOps sync (TOML files in a Git repository)

**Done when:** you can rebuild the whole Talaia configuration from a Git repository.

## Phase 9 · Version 1.0 (weeks 36–38) → 1.0.0

Polish, document and publish.

- Security review and load test (50 agents)
- Signed agent self-update
- User documentation and installation guide (EN/ES), UI fully translated (CA/ES/EN)
- arm64 support, signed packages and images

**Done when:** someone else installs it by following only the documentation.

---

## After 1.0

- Podman support
- RHEL family (Rocky, Alma, Fedora) and Alpine agents
- Preview environments per pull request
- Public status page
- Ansible playbook runner
- VPS provisioning through provider APIs (Hetzner, OVH…)
- Automatic monthly restore tests

## Risks

| Risk | Mitigation |
|---|---|
| **Scope creep** (the biggest risk) | Never start a phase before the previous one meets its "done when". New ideas go to [TODO.md](TODO.md) under *Ideas*. |
| **One key for every server** | Mandatory 2FA, panel behind a VPN, audit log, per-server module switches. |
| **Template maintenance** | Few, well-tested templates; external catalogs for the rest. |
| **Agent runs as root** | Typed commands only, modules can be disabled, signed updates. |
