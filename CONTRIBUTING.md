# Contributing to Talaia

[Leer en castellano](CONTRIBUTING.es.md)

Thank you for your interest in Talaia. The project is created and maintained by
**[CatLinux](https://github.com/catlinux)**, who makes the final decision on every change.

## Before you start

- Read the [README](README.md), the [roadmap](ROADMAP.md) and the [license](LICENSE) (WNCL-TP-1.0).
- Talaia is in an early phase. Work that is not in the current phase of the roadmap will probably not be
  accepted yet. If you have an idea, open an issue first.

## Reporting bugs and proposing ideas

- Search existing issues first.
- For bugs, include: Talaia version (`VERSION`), operating system, Docker version, steps to reproduce,
  what you expected and what happened.
- Security problems: **do not open a public issue**. Follow [SECURITY.md](SECURITY.md).

## Pull requests

1. Fork the repository and create a branch from `main`: `feat/short-name` or `fix/short-name`.
2. Keep each pull request focused on one change.
3. Follow the rules below. Pull requests that break them will be asked to change.
4. Fill in the description: what changes, why, how you tested it.

### Rules

- **Commits:** [Conventional Commits](https://www.conventionalcommits.org/) in English, for example
  `feat(agent): send heartbeat every 10 seconds`.
- **Languages in the software:** every user-facing string goes through i18n with translations in
  **Catalan (`ca`), Spanish (`es`) and English (`en`)**. See [docs/en/i18n.md](docs/en/i18n.md).
- **Languages in the documentation:** any documentation change must be made in **English and Spanish**.
- **Changelog:** add a line under *Unreleased* in `CHANGELOG.md` and `CHANGELOG.es.md`.
- **License:** license headers are applied with the maintainer's script. Do not write or edit them by hand.
- **Attribution:** do not remove or modify copyright notices, `LICENSE` or the in-app attribution to CatLinux.
- **Tests:** new logic must include tests, and all tests must pass.
- **Line endings:** LF only (configured in `.gitattributes`).

## Development environment

See [docs/en/development.md](docs/en/development.md).

## Versioning

The maintainer decides when to release. Versions follow SemVer `x.y.z`; see the table in the
[README](README.md#versioning).
