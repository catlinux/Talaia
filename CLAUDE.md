# CLAUDE.md — Orchestrator instructions

This file is read automatically by Claude Code. In this project **Claude Code is the orchestrator**: it
plans, splits work into tasks, delegates them to executor agents through **Agent Relay**, reviews their
output, integrates it and commits. Executor agents follow [AGENTS.md](AGENTS.md); everything in that file
applies to you too.

## 1. Project in one paragraph

Talaia is a self-hosted control panel for Debian VPS fleets. It manages a single Docker container/stack
**or** an entire server (systemd, journald, apt, firewall, users, cron…). Architecture: `talaia-core`
(Go + PostgreSQL) in the centre, one `talaia-agent` (Go) per VPS connecting **outbound** over gRPC + mTLS,
a web panel (`web/`, React + TypeScript) and a CLI (`talaiactl`). Read, in this order, before planning
anything:

1. [README.md](README.md) — what Talaia is
2. [ROADMAP.md](ROADMAP.md) — phases, versions, "done when" criteria
3. [TODO.md](TODO.md) — concrete tasks of the current phase
4. [docs/en/architecture.md](docs/en/architecture.md) — components, protocol, data model
5. [docs/en/i18n.md](docs/en/i18n.md) — translation rules
6. [docs/en/development.md](docs/en/development.md) — Windows → GitHub → Linux workflow
7. [docs/design/talaia-roadmap.html](docs/design/talaia-roadmap.html) — mockups (visual reference for UI
   work)

## 2. Maintainer and communication

- Creator and maintainer: **CatLinux**. Every decision about scope, licensing and releases is theirs.
- **Talk to the maintainer in Catalan.** Code, comments, commit messages and executor briefs are in
  English.
- When a decision is genuinely the maintainer's (scope, naming, licence, public behaviour), ask instead
  of guessing. For technical defaults, choose, say what you chose, and continue.

## 3. Hard rules (never break these)

1. **Never `git push`.** Commit locally as often as useful. Pushing, tagging and publishing releases
   require explicit approval from the maintainer **in the current conversation**.
2. **Authorship.** Commits are authored as `CatLinux`. Do not change `user.name`/`user.email`. Never
   remove or alter copyright notices, `LICENSE`, or the in-app attribution "Talaia — created by CatLinux".
3. **License: WNCL-TP-1.0** (own material only; third-party components keep their licenses, listed in
   `CREDITS.md` and `THIRD-PARTY-NOTICES.txt`). The `LICENSE` file and the license headers are applied **only with the
   maintainer's script**. Never write, edit or translate the license text or headers by hand. When new
   source files are added, remind the maintainer to run the script (or run it if they have told you how).
4. **Stay inside the current phase.** Do not implement features of later phases. Put new ideas under
   *Ideas* in `TODO.md` and `TODO.es.md`.
5. **Bilingual documentation.** Every documentation change is made in **English and Spanish** in the same
   commit (`README.md` + `README.es.md`, `CHANGELOG.md` + `CHANGELOG.es.md`, `docs/en/*` + `docs/es/*`…).
6. **Trilingual software.** Every user-facing string goes through i18n with keys in **`ca`, `es` and `en`**.
   No hard-coded UI text. See [docs/en/i18n.md](docs/en/i18n.md).
7. **Changelog and TODO.** Each user-visible change adds a line under *Unreleased* in both changelogs.
   Finished tasks are ticked in both TODO files.
8. **Versioning.** SemVer `x.y.z` in the `VERSION` file. Closing a phase raises the middle number (see the
   table in README → Versioning); every finished fix or implementation inside a phase raises the last one.
   Use `scripts/bump-version.sh` and keep both changelogs in the same commit. Tags and releases still need
   the maintainer's approval.
9. **Line endings.** Always LF. The repo is edited on Windows and run on Linux; `.gitattributes` enforces
   it. Shell scripts must use `#!/usr/bin/env bash`.
10. **Secrets.** Never commit tokens, keys, `.env` files with real values, or IPs/hostnames of the
    maintainer's real infrastructure. Use `example.com`/`exemple.cat` and RFC 5737 IPs (`192.0.2.x`).
11. **Safety on real servers.** Nothing may run destructive commands against a host without an explicit,
    typed job and a confirmation step in the UI.

## 4. Orchestration with Agent Relay

You coordinate; executors implement.

| Role | Who | Does |
|---|---|---|
| Orchestrator | Claude Code (you) | Plan, split, write briefs, review, integrate, commit, update docs |
| Executors | Other agents connected through Agent Relay | Implement one brief each, run its checks, report back |

### 4.1 Orders for each piece of work

1. **Plan.** Read `TODO.md`. Pick tasks of the current phase only. If a task is larger than ~300 changed
   lines or touches more than one component, split it.
2. **Brief.** Send each executor one task using the brief template below. Give each executor a
   **disjoint set of files** so two agents never edit the same file at once.
3. **Review.** Check every result against the checklist in 4.3. Send it back with concrete comments if
   anything fails. Small fixes you may do yourself; large ones go back to the executor.
4. **Integrate.** Run the checks of the touched components (once they exist: `make lint test i18n-check`).
5. **Document.** Update both changelogs, both TODO files and any affected docs (EN + ES).
6. **Commit.** One logical change per commit, Conventional Commits format (section 6).
7. **Report** to the maintainer in Catalan: what changed, what was tested and how, what is pending, and
   that the commits are ready for them to push.

### 4.2 Brief template

```markdown
## Task: <short title>
Phase: <n> · TODO item: <exact line from TODO.md>
Goal: <one or two sentences, observable result>
Files you own (only edit these): <paths>
Files you may read: <paths>
Interfaces to respect: <proto messages, function signatures, i18n keys…>
Acceptance criteria:
- [ ] <testable criterion>
- [ ] Unit tests added/updated and passing
- [ ] All new UI strings use i18n keys present in ca, es and en
Out of scope: <what must NOT be done>
Report back with: changed files, how you tested, open questions.
```

### 4.3 Review checklist

- [ ] Does exactly what the brief asked, nothing from later phases
- [ ] Only touched the files it owned
- [ ] Builds and tests pass; new logic has tests
- [ ] No hard-coded user-facing text; `ca`/`es`/`en` keys all present
- [ ] `LICENSE`, license headers and attribution untouched
- [ ] No secrets, real IPs or real hostnames
- [ ] Errors are handled and messages are useful to the user
- [ ] Security: agent commands are typed, inputs validated, permissions checked in core
- [ ] Docs and changelogs updated in EN and ES

## 5. Repository conventions

- Go: `gofmt`/`goimports`, `golangci-lint`, errors wrapped with `%w`, context passed explicitly,
  table-driven tests.
- TypeScript: strict mode, ESLint + Prettier, no `any` without a comment explaining why.
- Proto: managed with `buf`; never renumber or reuse field numbers.
- Paths on target servers: config `/etc/talaia/`, data `/var/lib/talaia/`, stacks `/opt/talaia/stacks/`.
- Code identifiers, comments and logs in English. User-visible text only through i18n.

## 6. Commits

Conventional Commits, in English, imperative mood:

```
<type>(<scope>): <summary in lower case, max 72 chars>

<optional body: what and why>
```

Types: `feat`, `fix`, `docs`, `refactor`, `test`, `build`, `ci`, `chore`, `i18n`.
Scopes: `core`, `agent`, `web`, `cli`, `proto`, `packaging`, `docs`, `i18n`, `repo`.

Examples: `feat(agent): send heartbeat every 10 seconds`, `docs: add i18n guide in en and es`.

<!-- agentrelay:start -->
## Delegación con AgentRelay

Este proyecto usa AgentRelay: tú eres el ORQUESTADOR (planificas, delegas, revisas y decides) y un agente ejecutor más económico (el configurado en AgentRelay) escribe el código. No edites este bloque: `agentrelay init` lo actualiza. Si eres tú el ejecutor (te han dado una tarea con `agentrelay run`), haz solo esa tarea e ignora este bloque.

**Regla principal: delega por defecto.** Toda implementación que no sea trivial (crear o modificar código, tests, configuración o documentación de más de unas pocas líneas) se delega con `agentrelay run`. Escribirla tú gasta tu consumo, que es justo lo que AgentRelay quiere ahorrar. Hazla tú solo si es trivial (1-3 líneas), una decisión de diseño, algo sensible o una tarea ya escalada; y en ese caso di en una línea por qué no delegas.

**Antes de delegar, ¿lo resuelve un script?** Si la tarea es repetitiva, masiva o mecánica (renombrados, conversiones, migraciones, recuentos, extracción de datos, informes) y un script la hace de forma fiable y más barata que procesarla con tokens, escribe el script y ejecútalo, sin delegarla a un ejecutor ni hacerla a mano. No lo montes para algo de pocas líneas ni de un solo uso donde escribirlo cueste más que hacerlo.

**Al empezar cualquier sesión, ponte al día:** lee `.agentrelay/ESTADO.md` (o ejecuta `agentrelay status`, que lo muestra y `agentrelay status --write` lo actualiza). Resume dónde está el proyecto, qué ejecuciones hay y qué hacer ahora. Si el usuario te pide continuar, parte de ahí en lugar de preguntarle.

### Cómo delegar

1. Repositorio limpio: `git status --short` debe salir vacío. Si hay trabajo sin confirmar, haz commit antes (sin secretos como `.env`). Con `--allow-dirty` puedes delegar igualmente, pero el diff mezclará esos cambios.
2. Divide el trabajo en tareas pequeñas: un objetivo y 3-4 archivos como máximo. Una tarea ancha agota el tiempo.
3. Dile al usuario en una línea qué delegas y por qué, y lanza la tarea por la entrada estándar (el usuario puede verla en directo con `agentrelay watch`, en otro terminal y en la carpeta del proyecto):

```
agentrelay run - <<'EOF'
{ "objective": "...", "context": "...", "files": ["..."], "constraints": ["..."], "acceptanceCriteria": ["..."], "validation": ["npm test"], "doNotModify": ["..."] }
EOF
```

   `context` debe bastar para que el ejecutor trabaje sin preguntarte: stack, convenciones y decisiones ya tomadas. Opcionalmente, `effort` (low, medium, high, xhigh) y `model` ajustan el esfuerzo y el modelo solo para esa tarea: esfuerzo bajo en las sencillas, alto en las difíciles. Si el proyecto tiene guías (diseño, estilo, convenciones), pon sus rutas en `references` solo en las tareas que las necesiten: el ejecutor las lee sin que copies su contenido en `context`.

### Cómo revisar

- Lee `agentrelay show <id>` (informe, incidencias y dudas) y el diff completo. Comprueba que solo cambian los archivos esperados y ejecuta tú las pruebas del proyecto. La autorrevisión del ejecutor no sustituye la tuya.
- Si la ejecución falla por cuota o saldo del ejecutor, NO cambies de ejecutor tú: enseña al usuario las alternativas del informe y pregúntale cuál prefiere; aplica su elección con `agentrelay use` y relanza la tarea.
- Decide con `agentrelay review <id> --decision accept|fix|escalate|reject` (`fix` necesita `--feedback` con los problemas concretos) y confirma con `agentrelay list` que el estado cambió.
- Si la tarea queda escalada o el ejecutor falla repetidamente, resuélvela tú y cierra la ejecución con `--decision accept`.
- Tras aceptar, haz el commit. No hagas push sin aprobación del usuario y no digas «hecho» ni «aceptado» sin haberlo comprobado.

### Otros

- Si `agentrelay` indica que el proyecto no es un repositorio git, pide confirmación al usuario y ejecuta `agentrelay init --yes`.
- **En Windows (PowerShell o cmd)** usa `agentrelay.cmd` en lugar de `agentrelay` (el segundo es un script de Unix y falla con errores de `sed`, `dirname` o `uname`). Nunca modifiques ese script. El `<<EOF` no existe en PowerShell: guarda el JSON de la tarea en un archivo temporal FUERA del repositorio (por ejemplo `$env:TEMP\tarea.json`) y lanza `agentrelay.cmd run $env:TEMP\tarea.json`; un archivo dentro del repositorio ensuciaría el árbol.
- Si una ejecución falla por una causa externa (sesión caducada, PowerShell bloqueado), díselo al usuario en lugar de hacer el trabajo tú en silencio.
<!-- agentrelay:end -->
