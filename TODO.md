# TODO

[Leer en castellano](TODO.es.md) · [Roadmap](ROADMAP.md)

Short-term task list. The [roadmap](ROADMAP.md) describes the phases; this file tracks the concrete tasks
of the current phase and the next one.

**Rule for agents and contributors:** when you finish a task, tick it here **and** in
[TODO.es.md](TODO.es.md) in the same commit.

---

## Now · Project definition (0.0.x)

- [x] Create the GitHub repository `catlinux/Talaia`
- [x] README (EN/ES)
- [x] ROADMAP (EN/ES)
- [x] TODO (EN/ES)
- [x] CHANGELOG (EN/ES)
- [x] CONTRIBUTING and SECURITY (EN/ES)
- [x] Agent instructions: `CLAUDE.md` (orchestrator) and `AGENTS.md` (executors)
- [x] Architecture, i18n and development guides (EN/ES)
- [x] Roadmap page and mockups in `docs/design/`
- [x] VS Code workspace settings and recommended extensions
- [ ] Set the GitHub repository description, website and topics
- [x] Apply the WNCL-1.0 license with the maintainer's script (LICENSE file and headers)
- [ ] Configure Agent Relay (orchestrator + executors) on the Windows machine
- [ ] Prepare the Linux test machine (Debian 13, Git, Docker, Go)
- [ ] Enable GitHub private vulnerability reporting

## Next · Phase 0: Foundations (0.1.0)

### Decisions to close
- [x] Database: PostgreSQL + TimescaleDB (SQLite discarded: the job queue and metrics history fit Postgres better)
- [x] Web framework: React + TypeScript (SvelteKit discarded: smaller ecosystem for terminal and editor components)
- [x] Minimum Go version: 1.26
- [x] Node.js 24 LTS and pnpm (pinned with Corepack through the `packageManager` field)

### Repository and CI
- [x] Create folders: `core/`, `agent/`, `proto/`, `web/`, `cli/`, `packaging/`, `test/e2e/`
- [x] Go workspace (`go.work`) with `core`, `agent`, `cli`
- [x] `Makefile` with `dev`, `build`, `test`, `lint`, `i18n-check`, `license-check`
- [ ] GitHub Actions: build, tests, lint, i18n check, license check
- [x] `scripts/bump-version.sh` that updates `VERSION` and both changelogs

### Contract
- [x] `proto/agent.proto` v0: `Heartbeat`, `Inventory`, `Metrics`
- [ ] Code generation with `buf`

### Core
- [x] HTTP server with `/healthz`
- [x] PostgreSQL connection and first migration
- [x] Configuration file and environment variables

### Agent
- [x] Minimal binary that connects to the core and sends hostname and CPU every 15 s
- [x] Configuration in `/etc/talaia/agent.toml`

### Web panel
- [x] Vite + React + TypeScript project
- [x] Design tokens and light/dark theme from the mockups
- [x] i18next with `ca`, `es`, `en` and language selector
- [x] "About" screen with the attribution "Talaia — created by CatLinux" (authorship of CatLinux)

### i18n
- [x] Locale file layout and key naming convention (see [docs/en/i18n.md](docs/en/i18n.md))
- [x] Script that fails if any key is missing in `ca`, `es` or `en`
- [x] go-i18n set up for core and CLI messages (the agent shows no text to users)

### Interface
- [ ] Credit the bundled fonts (Familjen Grotesk, IBM Plex Sans, JetBrains Mono, all SIL OFL 1.1) in the third-party section of the README (maintainer: credits are yours to approve)

### Test environment
- [ ] Two disposable VPS: Debian 12 and Debian 13
- [x] Clone-and-run guide for the Linux machine (`docs/en/installation.md`)
- [x] Verify `packaging/core/install.sh` and the agent download from the core end to end on a clean machine

## Phase 2 · What is left (see `.agents/plans/fase2/`)

- [ ] Terminal in a container, step 6 (what is left): a test with a real Docker on the Debian (the documentation is in `docs/en/security.md`)
- [ ] Terminal in a container, step 7: final security review of the whole diff (Opus)
- [x] Stack screen (mockup 3), live logs (`-f`) and a fixed note in the stack editor: a compose file runs as root on the server
- [ ] Close the phase against its criterion; closing it raises the version to 0.3.0 (maintainer policy, 2026-10-21)

## Operations block (decided 2026-10-20, before closing phase 2)

**Priority now (maintainer, 2026-10-20):** the updates from the panel and the external access, to keep updating and managing from outside the home. Design and steps: [`.agents/plans/operacion/operacion.PLAN.md`](.agents/plans/operacion/operacion.PLAN.md).

**Principle:** Talaia is installed from GitHub on machines we do not know, so it must not depend on what is already there: no reverse proxy, no free ports 80/443, no third-party VPN service.

- [x] Two ports of its own, chosen free by the installer (8443 and 8444 by default, never 80/443 unless free and asked for): one for the panel, one for the agents
- [x] Installer that checks the ports, asks for the public name and writes `.env`
- [x] Trusted certificate for the panel by ACME DNS-01 inside the core (lego; OVH first), on any port; the agents keep pinning Talaia's own CA
- [x] Update detection (latest commit of `main` with green CI) and API
- [x] Host helper `talaia-hostd` (systemd) that pulls, rebuilds, checks `/healthz` and rolls back with a database copy; the core never gets the Docker socket
- [x] Settings screen with the version and the update button; ca/es/en
- [x] Documentation EN/ES (VPS with other services, OVH credential with minimal rights, updates)
- [x] Security review (Opus)
- [x] A request the helper never takes (helper stopped or not installed by hand) leaves the panel showing "update in progress": report a request older than a few minutes as stuck
- [x] The helper does not update itself: an update that changes `packaging/core/hostd/` needs `install.sh` run again; do it from the helper or warn in the panel
- [x] Real install at `talaia.invierteconcriterio.es` with the maintainer (fresh install; the Debian at home becomes one more server)
- [ ] Later: change the core address of the agents with a typed command (to move to the new domain and VPS)

## Ideas (not planned yet)

Write new ideas here instead of adding them to the current phase.

- Mobile app (PWA) with push notifications
- Integration with Tailscale/Headscale to create the private network automatically
- Cost dashboard per provider
- Future option: Headscale or another VPN, to keep the panel or some machines private (decided by the maintainer on 2026-10-20 to leave it for later). Set aside for now: Headscale wants port 443 in production, which breaks the principle above, and a public core already reaches servers behind NAT because agents connect outward
