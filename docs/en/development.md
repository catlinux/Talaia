# Development workflow

[Leer en castellano](../es/desarrollo.md) · [Back to README](../../README.md)

## 1. The cycle

```
┌──────────────────────────────┐   commit    ┌──────────────┐
│ Windows                      │ ──────────▶ │  local repo  │
│ VS Code + Claude Code        │             └──────┬───────┘
│ + Agent Relay executors      │                    │ git push (only after the
└──────────────────────────────┘                    │ maintainer approves)
                                                    ▼
                                             ┌──────────────┐
                                             │    GitHub    │
                                             │ catlinux/    │
                                             │ Talaia       │
                                             └──────┬───────┘
                                                    │ git clone / git pull
                                                    ▼
                                      ┌───────────────────────────┐
                                      │ Linux test machine        │
                                      │ Debian 13: build and test │
                                      └───────────────────────────┘
```

1. Work locally on Windows with VS Code and Claude Code.
2. Claude Code (orchestrator) splits work and delegates to executor agents through Agent Relay.
3. Changes are committed locally. Commits can happen at any time.
4. **The maintainer approves every push.** Agents never push on their own.
5. On the Linux machine, `git pull` and test.

## 2. Windows machine

The Windows instructions have been removed: Claude Code already has the working guidelines for local work, remote work and GitHub repositories.

> `.gitattributes` forces LF line endings for all text files, so files edited on Windows run correctly on
> Linux.

Optional: WSL2 with Debian lets you run Linux builds and tests on the same Windows machine.

## 3. Linux test machine

Debian 13.

```bash
sudo apt update
sudo apt install -y git make curl ca-certificates
# Docker Engine: https://docs.docker.com/engine/install/debian/
# Go and Node.js: versions fixed in phase 0

git clone https://github.com/catlinux/Talaia.git
cd Talaia
```

To get new changes:

```bash
git pull
```

Test commands will be documented here from phase 0 (`make dev`, `make test`).

## 4. Branches

- `main` is always in a working state.
- While there is a single maintainer, small changes can go directly to `main`.
- Larger changes: `feat/<name>` or `fix/<name>` branches, merged into `main` when they pass the checks.

## 5. Releases

1. All tasks of the phase are ticked in `TODO.md` / `TODO.es.md` and the "done when" criterion is met.
2. Update `VERSION`.
3. In both changelogs, move *Unreleased* to a new section `[x.y.z] - YYYY-MM-DD`.
4. Commit: `chore(release): x.y.z`.
5. The maintainer creates the tag `vx.y.z` and pushes.

## 6. Working with AI agents

- [CLAUDE.md](../../CLAUDE.md): rules for the orchestrator (Claude Code).
- [AGENTS.md](../../AGENTS.md): rules for executor agents.

The orchestrator gives each executor a brief with a disjoint set of files, reviews every result and makes
the commits. Executors never commit or push.
