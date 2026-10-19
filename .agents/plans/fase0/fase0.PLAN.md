# Fase 0 · Fundamentos (0.1.0)

Estado: **pendiente de aprobación del mantenedor**. No se programa nada hasta que se apruebe.

Decisiones cerradas: PostgreSQL 16 + TimescaleDB · React + TypeScript (Vite, pnpm) · Go 1.26 · Node.js 24 LTS.
Reglas: `CLAUDE.md` (hay que cumplirlas). Criterio de la fase: `make dev` levanta core + panel + agente vacío en local y el CI está en verde.

Ejecutores: AgentRelay (`agentrelay.cmd` en PowerShell, JSON de la tarea en `$env:TEMP`, fuera del repo). Cada tarea usa la plantilla de `CLAUDE.md` §4.2 con un conjunto de archivos propio y disjunto. Una tarea → un commit (Conventional Commits, inglés). Ningún paso sube la versión; al cerrar la fase se sube `VERSION` a 0.1.0 (menor: añade funcionalidad compatible) y se mueve *Sin publicar* a `[0.1.0]` en los dos CHANGELOG.

| # | Tarea | Quién | Archivos | Terminada cuando | Depende de |
|---|---|---|---|---|---|
| 1 ✅ | Contrato `proto/agent.proto` v0 (`AgentHub.Connect`, `Heartbeat`, `Inventory`, `Metrics`) y `buf.yaml`/`buf.gen.yaml` | **Orquestador** (diseño delicado: no se renumeran campos) | `proto/**` | `buf lint` y `buf generate` sin errores | — |
| 2 ✅ | Esqueleto del repo: carpetas, `go.work`, `go.mod` de `core`/`agent`/`cli`, `Makefile` (`dev build test lint i18n-check license-check`), `scripts/bump-version.sh` | Ejecutor · esfuerzo bajo | `go.work`, `*/go.mod`, `Makefile`, `scripts/bump-version.sh` | `make build` compila los módulos vacíos; el script actualiza `VERSION` y ambos CHANGELOG | — |
| 3 ✅ | Core: servidor HTTP con `/healthz`, configuración (archivo + variables de entorno), conexión a PostgreSQL y primera migración | Ejecutor · esfuerzo medio | `core/**` (salvo `core/internal/hub/`) | `go test ./core/...` verde; `/healthz` responde 200 con la base de datos arriba | 2 |
| 4 ✅ | Hub mínimo en el core y agente mínimo: el agente se conecta y envía hostname y CPU cada 15 s; configuración en `/etc/talaia/agent.toml` | Ejecutor · esfuerzo medio | `core/internal/hub/**`, `agent/**` | prueba de integración local: el core registra los latidos del agente | 1, 3 |
| 5 ✅ | Panel: Vite + React + TypeScript, tokens de diseño y tema claro/oscuro tomados de las maquetas, pantalla «Acerca de» con «Talaia — created by CatLinux» | Ejecutor · esfuerzo medio | `web/**` (sin `web/src/i18n/`) | `pnpm build` y `pnpm lint` verdes | 2 |
| 6 ✅ | i18n restante (i18next y selector ya están en la 5): go-i18n en core y CLI (`core/i18n/locales`, `cli/i18n/locales`) y `scripts/i18n-check.mjs` (Node) que compara las claves de ca/es/en en web y Go | Ejecutor · esfuerzo medio | `core/i18n/**`, `cli/i18n/**`, `scripts/i18n-check.mjs` | el script falla al quitar una clave de un idioma y pasa con todas | 5 |
| 7 ✅ | Entorno de desarrollo: `make dev` (compose con Postgres + core + panel + agente) | Ejecutor · esfuerzo medio | `packaging/dev/**`, objetivo `dev` del `Makefile` | `make dev` levanta todo en local | 3, 4, 5 |
| 8 ✅ | CI en GitHub Actions: build, tests, lint, i18n-check y license-check | Ejecutor · esfuerzo bajo | `.github/workflows/**` | workflow en verde (se consulta con la API pública, sin login) | 2, 6 |
| 9 | Integración, revisión de cada tarea, cierre de la fase (0.1.0, CHANGELOG, TODO) | **Orquestador** | docs | criterio de la fase cumplido | todas |
| M | Manual del mantenedor: dos VPS desechables (Debian 12 y 13), descripción/temas del repo, *private vulnerability reporting*, integrar el script de licencias en `license-check` | Mantenedor | — | marcado en `TODO.md` | — |

## Notas

- CI escrito (`.github/workflows/ci.yml`, `.golangci.yml`); el lint se verificó en local con golangci-lint v2.15.0 (`C:/Users/msanc/tools/bin`). El CI pasó en verde en GitHub con el commit da12eb5 (10 jobs). Falta el trabajo `license-check` (depende del script de licencias del mantenedor). Riesgo conocido: el job `proto` regenera el código con plugins remotos de buf; si cambian de versión puede dar diferencias.

- **Sin verificar:** `make dev` / `scripts/dev.sh` y `packaging/dev/compose.yaml` solo pasan `bash -n`; falta ejecutarlos en el Debian de pruebas (Docker, Go 1.26, Node 24, pnpm). Es el criterio de cierre de la fase.

- Para la fase 1: el hub registra los agentes por hostname; al reconectar antes de que caiga el stream viejo, éste borra el registro nuevo. Pasar a ID de servidor y borrar solo si la conexión es la misma. El servidor HTTP no tiene ReadTimeout/WriteTimeout (cortarían el stream); poner límites por petición en la API REST.

- AgentRelay ejecuta las validaciones con `cmd.exe` en Windows: nada de `test`, `$(...)` ni builtins POSIX en `validation`; solo comandos `go`, y pasar el PATH de Go (C:/Users/msanc/tools/go1.26.9/bin) al lanzar `agentrelay.cmd`. El gofmt se comprueba a mano. Si la validación falla por el entorno y el código está verificado a mano, `review --decision accept --force`.

- Tareas 1 y 2 hechas y verificadas el 2026-10-19: `buf lint`, `buf generate` y `go build`/`go vet` de los cuatro módulos pasan. Herramientas locales sin administrador en `C:\Users\msanc\tools` (Go 1.26.9 en `go1.26.9\bin`, `buf.exe` en `bin`); no hay `make` en Windows: se usan los comandos `go` directamente.

- Orden: 1 y 2 en paralelo → 3 y 5 en paralelo → 4 y 6 en paralelo → 7 y 8.
- Los ficheros nuevos de código necesitan cabeceras de licencia (WNCL-1.0). Hay que acordar el procedimiento antes de la tarea 3 (CLAUDE.md §3 regla 3).
- Si una tarea topa con una decisión de diseño, el ejecutor para y lo anota aquí; lo resuelve el orquestador.

## Cómo seguir

`/clear` → `/model opus` (tareas 1 y 9) o `/model sonnet` → «ejecuta las tareas 1 y 2 de `fase0.PLAN.md`».
