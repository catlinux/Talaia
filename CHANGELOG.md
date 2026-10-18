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

### Changed

- Phase 0 decisions closed: PostgreSQL + TimescaleDB, React + TypeScript, Go 1.26, Node.js 24 LTS with pnpm.

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
