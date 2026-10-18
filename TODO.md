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

### Test environment
- [ ] Two disposable VPS: Debian 12 and Debian 13
- [ ] Clone-and-run guide for the Linux machine

## Ideas (not planned yet)

Write new ideas here instead of adding them to the current phase.

- Mobile app (PWA) with push notifications
- Integration with Tailscale/Headscale to create the private network automatically
- Cost dashboard per provider
