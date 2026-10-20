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
- [ ] Verify `packaging/core/install.sh` and the agent download from the core end to end on a clean machine

## Phase 2 · What is left (see `.agents/plans/fase2/`)

- [ ] Terminal in a container, step 6: documentation EN/ES (security: the administrator is root on the servers; recording and retention; `terminal export`) and a test with a real Docker on the Debian
- [ ] Terminal in a container, step 7: final security review of the whole diff (Opus)
- [ ] Stack screen (mockup 3), live logs (`-f`) and a fixed note in the stack editor: a compose file runs as root on the server
- [ ] Close the phase against its criterion; version 0.3.0 only if the maintainer asks

## Operations block (decided 2026-10-20, before closing phase 2)

Design first with a powerful model (Opus), written to `.agents/plans/operacion/operacion.PLAN.md`; then run it with a cheaper one.

### Updates from the panel
- [ ] Host helper `talaia-hostd` (systemd, root) with a closed list of operations. The core only writes request files into its data volume; it never gets the Docker socket (the container is read-only and without privileges on purpose)
- [ ] Detect: the core asks GitHub for the latest commit of `main` about every 30 minutes (conditional requests), compares it with the commit baked into its image at build time, and offers only commits whose CI is green
- [ ] Apply: `git pull --ff-only` and `docker compose up -d --build`, wait for `/healthz`, go back to the previous commit if it does not answer (database migrations only go forward: say so in the plan)
- [ ] Settings screen (there is none yet): version card, "N new commits", an update button with confirmation, progress and result; ca/es/en
- [ ] Documentation: install the helper once on the Debian with `sudo`; after that no commands are needed
- [ ] Later: update the agents of the other servers from the core (today: `server upgrade-command` pasted by hand)

### Remote access and trusted HTTPS (decided by the maintainer: Headscale with their own domain)
- [ ] Headscale on one of the VPS (public IP), run natively and not in a container (its maintainers do not recommend containers or reverse proxies: check the current documentation); Tailscale clients on the core machine, the VPS, the PC and the phone. Open question: which VPS hosts it; if it is down, new connections cannot start
- [ ] Own domain on OVH: an A record such as `panel.<domain>` pointing to the private VPN address of the core, and a Let's Encrypt certificate by DNS-01 through the OVH API with a credential limited to that zone. Open question: which domain and subdomain, and whether the zone is hosted at OVH
- [ ] Renew the certificate from the host helper and let the core reload it from disk
- [ ] Core: present the valid certificate to browsers and the certificate of its own CA to agents, according to the name they ask for; agents connect through the VPN address
- [ ] Limit what each VPS may reach in the VPN to the core port
- [ ] Documentation EN/ES: the core stays network-agnostic; Headscale is the recommended way, with WireGuard or an open port as alternatives. Replace the untested section 6 of the installation guide

## Ideas (not planned yet)

Write new ideas here instead of adding them to the current phase.

- Mobile app (PWA) with push notifications
- Integration with Tailscale/Headscale to create the private network automatically
- Cost dashboard per provider
