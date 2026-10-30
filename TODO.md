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

- [x] Terminal in a container, step 6 (what is left): a test with a real Docker on the Debian (the documentation is in `docs/en/security.md`)
- [x] Terminal in a container, step 7: final security review of the whole diff (Opus)
- [x] Stack screen (mockup 3), live logs (`-f`) and a fixed note in the stack editor: a compose file runs as root on the server
- [x] An adopted stack whose directory has disappeared stays as a broken entry: detect it and offer **Stop managing** (or clean the record) in the panel; found in the real test of 2026-10-22
- [x] Close the phase against its criterion; closing it raises the version to 0.3.0 (maintainer policy, 2026-10-21)

## Phase 3 · Whole server (0.4.0, see `.agents/plans/fase3/fase3.PLAN.md`)

- [x] Contract for apt, systemd, journald, reboot and the host terminal
- [x] apt, read only: pending and security updates, held packages, "reboot required" (agent, core, Updates tab, fleet badges)
- [x] apt upgrade (all or security only) that survives an agent restart, and reboot with the server name typed
- [x] systemd: enabled state, start/stop/restart/reload/enable/disable with confirmation, Services tab
- [x] journald: filters (unit, priority, boot, time, text) and follow, Logs tab
- [x] Host terminal: root shell with a fresh TOTP code, recording, red banner
- [x] Server screen with tabs and overview (mockup 2) and audit log screen
- [x] Docs EN/ES, security review (Opus), real test on two servers, close at 0.4.0 (closed by the maintainer, 2026-10-22; the week without SSH goes on in daily use)

## Phase 4 · Users, permissions and audit (0.5.0, see `.agents/plans/fase4/fase4.PLAN.md`)

- [x] Permission model: fixed roles (admin, server operator, app operator, viewer, metrics only) on organization → group → server → stack → container; a stack binding never reaches the host
- [x] Users, teams, server groups and invitations by link (2FA mandatory for everyone)
- [x] Permissions checked on every API route, with a test that fails if a route has none; filtered lists
- [x] Scoped API keys (never terminals, users or reboot) and `talaiactl` v1
- [x] OIDC sign-in for existing users (verified email), second factor still required
- [x] Panel: Users and permissions screen (mockup 5), API keys, OIDC settings, terminal recordings list and download
- [x] Terminal session recording (done in phases 2 and 3)
- [ ] After an update, the panel keeps showing the old version: make it reload itself (or show a "reload" notice) once the core restarts with a new version
- [x] Docs EN/ES, security review (done by Sonnet, to repeat with Opus if wanted), real test with a second user, close at 0.5.0 (closed by the maintainer, 2026-10-24)
- [ ] Security follow-ups of the phase 4 review (Opus, 2026-10-24), all low:
  - [ ] OIDC: when no public URL is configured the redirect URI is built from the request `Host`; refuse to enable OIDC without a public URL (the identity provider's redirect allow-list already blocks abuse)
  - [ ] OIDC: the `http://localhost` issuer exception exists for tests; allow it only in development builds so an administrator cannot point discovery at local services
  - [ ] Every denied request writes an `authz_denied` audit row: rate-limit or aggregate them per user so a signed-in user cannot grow the audit table at will
  - [ ] Jobs remember their creator by username; store the user id too so a deleted user's name, reused later, never reaches old job output

## Phase 5 · Monitoring and alerts (0.6.0, see `.agents/plans/fase5/fase5.PLAN.md`)

- [x] Agent: container health and Docker events so the inventory is sent within seconds, not 60 s
- [x] Metrics history in TimescaleDB (Apache-2 features only), downsampling and retention done by the core, and its API
- [x] CPU, RAM, disk and network charts for the server and per container
- [x] Alert permissions and tables for rules, incidents, silences, checks and channels (with default rules: server offline, disk > 90%, container stopped)
- [x] Outbound guard against SSRF for checks and notifications
- [x] Alert engine: states, hysteresis, no repeated notifications, silences and expected stops from Talaia
- [x] HTTP/TCP/TLS checks from the core and certificate expiry
- [x] Notifications through Telegram, ntfy, email, Discord and signed webhook, with encrypted secrets and retries
- [x] API and panel: Alerts screen, count in the fleet view and Settings for rules, checks and channels
- [x] Docs EN/ES, real test of the criterion and close at 0.6.0 (the Opus security review is done: 0.5.14; the stopped-container test passed with Telegram on 2026-10-28; the disk test was skipped; the engine tests cover the disk rule)
- [ ] Follow-ups of the phase 5 review (Opus, 2026-10-25), all low:
  - [ ] The engine reads rules, silences, open incidents and checks from the database on every evaluation (every 5 s and on each inventory): cache them and invalidate on change if fleets grow
  - [ ] Container stop/remove from the API tells the engine when the job is accepted, not when it succeeds; a failed stop is watched again after 2 minutes
  - [ ] The server **Alerts** tab does not show group-scoped rules as inherited: the panel's `Server` type carries no group, so `AlertsSection` never receives `groupId`

## Panel v2 · professional panel and easier alerts (between phase 6 steps 4 and 5, see `.agents/plans/panel-v2/panel-v2.PLAN.md`)

- [x] Static mockup of the new design, approved by the maintainer
- [x] Sober design tokens and base styles (no dotted backgrounds, glows or gradients)
- [x] Shared components: page header, data table, status badge, drawer, menu, toast, confirmation
- [x] Real URLs, grouped sidebar and top bar (breadcrumbs, alert bell, user menu)
- [x] Overview with "needs attention" first; servers as a table
- [ ] Server page with a fixed header and tabs as routes
- [ ] Fleet-wide containers and stacks with row and bulk actions
- [ ] "What is this?" catalogue: description, importance and what to do if it fails for common Debian services and Docker images, as a tooltip in the tables
- [ ] Alerts inbox: filters, grouping, new/seen/silenced, bulk actions, detail drawer with actions on the cause
- [ ] Alert rules, channels and checks moved from Settings to Alerts
- [ ] Command palette (Ctrl+K); Users, Audit and Settings on the new components; mobile pass and docs

## Phase 6 · Deploys, domains and templates (0.7.0, see `.agents/plans/fase6/fase6.PLAN.md`)

Decisions closed on 2026-10-30: external proxy by default (Talaia never touches Apache or other services on the server), Dockerfile only, GitHub only, nothing installed on the real VPS until the final test.

- [x] Contract: typed commands for Git source, build, release and proxy
- [x] Apps, environment variables, deployments and domains in the database; `server.proxy` permission
- [x] Agent: hardened Git fetch from GitHub with per-app deploy keys and a pinned host key
- [x] Core: signed GitHub push webhooks and branch polling
- [ ] Agent: builds with a Dockerfile, one at a time per server
- [ ] Agent: external proxy by default (app on a free `127.0.0.1` port, Apache/nginx block shown in the panel) and optional managed Caddy only when 80/443 are free
- [ ] Agent: zero-downtime release, rollback to one of the last three images and image cleanup
- [ ] Core: deploy pipeline (one deploy per app, superseded pushes, logs) and its API
- [ ] Template catalog (15 apps) shipped with the core
- [ ] Panel: Apps, new app wizard (mockup 4), Domains and Templates
- [ ] Docs EN/ES, Opus security review, real test of the criterion and close at 0.7.0

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

- Own notes per container or service (for example "shop website, do not touch during opening hours"), stored in the core
- Builds without a Dockerfile (Nixpacks/Railpack), GitLab and Gitea/Forgejo webhooks, and writing the Apache/nginx configuration automatically (left out of phase 6 on 2026-10-30)
- Mobile app (PWA) with push notifications
- Integration with Tailscale/Headscale to create the private network automatically
- Cost dashboard per provider
- Future option: Headscale or another VPN, to keep the panel or some machines private (decided by the maintainer on 2026-10-20 to leave it for later). Set aside for now: Headscale wants port 443 in production, which breaks the principle above, and a public core already reaches servers behind NAT because agents connect outward
- Option to update the agents automatically after the core is updated (opt-in, one server at a time, stopping at the first failure), instead of the separate "update all agents" step
- Optional redirect from plain HTTP to the panel's HTTPS address, so typing the host without `https://` or the port lands on the panel
