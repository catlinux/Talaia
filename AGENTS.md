# AGENTS.md — Executor instructions

This file is for every AI agent that works on Talaia as an **executor**. Work is assigned by the
**orchestrator** (Claude Code) through Agent Relay. The orchestrator's rules are in
[CLAUDE.md](CLAUDE.md).

## What Talaia is

A self-hosted control panel for Debian VPS fleets. It manages one container/stack or a whole server.
Components: `core/` (Go + PostgreSQL), `agent/` (Go, one per VPS, outbound gRPC + mTLS), `web/` (React +
TypeScript), `cli/` (`talaiactl`), `proto/` (shared contract). Details:
[docs/en/architecture.md](docs/en/architecture.md).

## Orders

1. **Do only your brief.** Implement exactly the task the orchestrator gave you. If something outside it
   looks wrong, mention it in your report; do not fix it.
2. **Edit only the files you own** (listed in the brief). Read anything else you need.
3. **Do not commit, push, tag, or change git configuration.** The orchestrator integrates and commits.
4. **Ask instead of guessing** when the brief is ambiguous about behaviour or interfaces. Send the
   question to the orchestrator and continue with any part that is not blocked.
5. **Report back** with: list of changed files, how you tested (commands and results), anything left
   undone, and open questions.

## Rules

- **i18n:** never hard-code user-facing text. Add the key to the `ca`, `es` **and** `en` locale files.
  If you are unsure of a Catalan or Spanish translation, write your best version and flag it in the
  report. See [docs/en/i18n.md](docs/en/i18n.md).
- **Docs:** if you change documentation, change both the English and Spanish versions.
- **License (WNCL-TP-1.0):** never write, edit or remove `LICENSE`, license headers, copyright notices or
  the in-app attribution "Talaia — created by CatLinux". Headers are added by the maintainer's script.
  After adding, removing or upgrading a dependency, run `node scripts/third-party-notices.mjs` and update
  `CREDITS.md` and `CREDITS.es.md` (CI fails if `THIRD-PARTY-NOTICES.txt` is out of date).
- **Tests:** new logic comes with tests. Run the tests of the component you touched before reporting.
- **Line endings:** LF only. Shell scripts start with `#!/usr/bin/env bash`.
- **No secrets:** no real tokens, keys, IPs or hostnames. Use `example.com` and `192.0.2.x`.
- **Phase scope:** do not add features from later roadmap phases.
- **Security:** the agent only executes typed commands; validate every input; permission checks live in
  `core`, never only in the web panel.

## Code style

- Go: `gofmt`, `golangci-lint`, wrap errors with `%w`, pass `context.Context`, table-driven tests.
- TypeScript: strict mode, ESLint + Prettier.
- Proto: never renumber or reuse field numbers.
- Identifiers, comments and logs in English.

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
