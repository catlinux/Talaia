# Changelog

[Leer en castellano](CHANGELOG.es.md)

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). The current version is in the
[`VERSION`](VERSION) file.

Sections used in each release: **Added**, **Changed**, **Deprecated**, **Removed**, **Fixed**,
**Security**.

## [Unreleased]

### Added

- Monorepo skeleton: Go workspace, empty `core`, `agent` and `cli` modules, `Makefile` and `scripts/bump-version.sh`.
- Agent contract `proto/agent.proto` v0 (`Heartbeat`, `Inventory`, `Metrics`) with `buf` configuration.
- Core skeleton: HTTP server with `/healthz`, TOML + environment configuration, PostgreSQL connection and first migration.
- Web panel skeleton: Vite + React + TypeScript, design tokens with light/dark theme, i18next (ca/es/en) with language selector and About screen.
- QR code on the second-factor setup screen, drawn as inline SVG so it works under the panel's CSP.
- The core serves `install.sh` and the agent packages (amd64 and arm64, built into its image) and `token create` prints the complete install command, so a new server needs one pasted line and no package copied by hand. `install.sh` checks the CA fingerprint before it trusts the package, and `--deb` is now optional.
- New settings `TALAIA_DIST_DIR` and `TALAIA_PUBLIC_PORT` (`dist_dir`, `public_port`); the public port also feeds the default allowed origins, which now work with any published port.
- `packaging/core/install.sh`: sets up the core in one command (random database password in `.env`, build, start, administrator), and the installation guide `docs/en/installation.md`.
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

- Security review of the enrollment: revocation now closes live agent streams, the agent identity is bound to the current or previous certificate serial, enrollment is a single transaction, the agent key and certificate are stored in one atomic file, `insecure_dev` only listens on loopback, permanent agent errors stop systemd restarts (exit 78), and the CA and certificate directories are forced to mode 0700.
- Panel authentication (ahead of phase 3): single administrator created from the console, argon2id password, mandatory TOTP with single-use recovery codes, secure session cookie, CSRF protection, brute-force limits, audit log, and the sign-in screens in ca/es/en.
- Core deployment with Docker Compose (`packaging/core/`): one image with the core and the web panel, PostgreSQL 16 + TimescaleDB, read-only root filesystem and no capabilities.
- `GET /api/v1/servers` (full session required) and the fleet screen fed by it, refreshed every 15 seconds; data that does not exist yet is shown as not available instead of sample numbers.

### Changed

- Phase 0 decisions closed: PostgreSQL + TimescaleDB, React + TypeScript, Go 1.26, Node.js 24 LTS with pnpm.
- The session that waits for the second factor lasts 15 minutes at the first access (it was 5), enough to install an authenticator app and scan the QR code; users that already have the second factor keep 5 minutes. The sign-in screen now says when the session expired.

### Fixed

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

[Unreleased]: https://github.com/catlinux/Talaia/compare/v0.0.1...HEAD
[0.0.1]: https://github.com/catlinux/Talaia/releases/tag/v0.0.1
