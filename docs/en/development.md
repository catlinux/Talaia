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
# Go 1.26 and Node.js 24 LTS with pnpm (installation steps arrive in phase 0)

git clone https://github.com/catlinux/Talaia.git
cd Talaia
```

To get new changes:

```bash
git pull
```

Commands (requirements: Docker Engine with Compose v2, Go 1.26, Node.js 24 with pnpm):

```bash
make dev           # PostgreSQL + TimescaleDB (Docker), core, an agent and the web panel
make test          # Go tests of core, agent and cli
make lint          # golangci-lint
make i18n-check    # translation keys in ca, es and en
bash scripts/dev.sh down   # stop the database (add -v to remove its data)
```

`make dev` publishes the core on `http://127.0.0.1:8080` (`/healthz`) and the web panel on the Vite port
shown in its output. The credentials in `packaging/dev/compose.yaml` are for local development only.

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
