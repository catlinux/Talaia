# Fase 3 · Servidor entero (0.4.0 · MVP)

Estado: **diseño de Opus (2026-10-22), sin código salvo el contrato del paso 1.** Criterio del ROADMAP: *«pasas una
semana entera sin entrar por SSH en tus VPS»*. Orden pedido por el mantenedor: apt (solo lectura) → systemd →
journald → terminal del host.

Punto de partida: trabajos tipados con `job_id` idempotente y `JobEvent` (`D-trabajos.DESIGN.md`), caducidad por
tipo en `core/internal/api/containers.go` (`jobTimeout`), inventario en memoria del hub (`AgentInfo`), el
inventario ya lista los servicios (`agent/internal/inventory/systemd.go`), terminal de contenedor con grabación
(`D-terminal.DESIGN.md`), registros con seguimiento por sondeo (`LogsPanel.tsx`, cada 3 s). El agente corre como
root. La autenticación (2FA, auditoría) ya está hecha; falta la pantalla de auditoría.

Reglas de toda la fase (CLAUDE.md §3): órdenes **tipadas**, el agente nunca construye una línea de shell (siempre
`exec` con argumentos); el core solo acepta nombres que el agente haya informado; lo destructivo pide `confirm`;
textos por i18n ca/es/en; docs EN+ES; estilo según la memoria `feedback-estilo-visual`; una dependencia nueva
obliga a `node scripts/third-party-notices.mjs` y a actualizar `CREDITS.md`/`CREDITS.es.md`.

## Decisiones

### D1 · apt
- **Resumen periódico en el inventario** (`Inventory.apt`, `AptSummary`): el agente simula al arrancar, cada 6 h
  y tras cada trabajo de apt: `apt-get -s -o Debug::NoLocking=1 upgrade --with-new-pkgs` (solo lectura, sin
  bloqueo). **No** ejecuta `apt-get update` por su cuenta (lo hace `apt-daily.timer`); informa de la fecha de las
  listas (mtime de `/var/lib/apt/lists/partial` o del más reciente de `/var/lib/apt/lists/*_Release`/`*_InRelease`).
- **Seguridad** = la línea `Inst` tiene un origen con `-security` (`Debian-Security:13/stable-security`,
  `…/jammy-security`). Formato: `Inst <pkg> [<actual>] (<candidata> <origen> [<arq>])`; `[<actual>]` falta en
  paquetes nuevos (dependencias nuevas, p. ej. un kernel).
- **Reinicio necesario** = existe `/run/reboot-required` **o** el kernel instalado más nuevo (`/lib/modules/*`,
  orden de versiones Debian) no es el que corre (`uname -r`). Se informa el motivo.
- `AptStatus{refresh}`: lista completa como JSON (`packages[{name,current,candidate,origin,security}]`,
  `held[]` de `apt-mark showhold`, `reboot_required`, `reboot_reasons[]`, `lists_updated_at`, `checked_at`).
  `refresh=true` ejecuta antes `apt-get update` (caducidad 10 min).
- `AptUpgrade{mode}`: `ALL` = `apt-get -y upgrade --with-new-pkgs` (nunca elimina paquetes; `full-upgrade` queda
  fuera de la fase); `SECURITY` = `apt-get -y install --only-upgrade <paquetes>` con la lista que **calcula el
  agente** en ese momento (el core no manda nombres). Siempre con `DEBIAN_FRONTEND=noninteractive`,
  `-o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold` (conserva la configuración local) y
  `-o DPkg::Lock::Timeout=120`. Nombres validados con `^[a-z0-9][a-z0-9+.-]*(:[a-z0-9]+)?$`.
- **Sobrevive a un reinicio del agente**: la orden corre en una unidad transitoria
  `systemd-run --unit=talaia-apt-<8 primeros del job_id> --collect --property=Type=exec
  --property=StandardOutput=append:/var/lib/talaia/apt/<job_id>.log --property=StandardError=… --setenv=…`.
  El agente guarda `/var/lib/talaia/apt/<job_id>.json` (job_id, unidad), sondea `systemctl show -p
  ActiveState,Result,ExecMainStatus` cada 2 s y manda `RUNNING` con la cola del log (64 KiB) cada 5 s. Al
  arrancar retoma las unidades pendientes y manda el resultado (el core lo ignora si el trabajo ya se cerró).
  Un único trabajo de apt a la vez por servidor (mutex del agente; si no, `FAILED` «hay otra operación de apt»).
  Logs de más de 30 días se borran.
- `HostReboot{hostname}`: el agente comprueba que `hostname` es el suyo, responde `SUCCEEDED` y programa
  `systemd-run --on-active=5 systemctl reboot`. **Sin apagado** (no se podría volver a encender desde Talaia).
  El panel pide escribir el nombre del servidor; el core exige `confirm` igual al nombre.
- Caducidades en `jobTimeout`: `apt_status` 2 min (10 con `refresh`), `apt_upgrade` 60 min, `host_reboot` 1 min.
- El panel avisa si la lista incluye `docker-ce`, `docker.io`, `containerd` o `containerd.io` (reinicia los
  contenedores) y si hay paquetes retenidos.

### D2 · systemd
- `SystemdUnit.unit_file_state` (`enabled`, `disabled`, `static`, `masked`…) a partir de `systemctl
  list-unit-files --type=service --output=json`, unido por nombre. Solo servicios en esta fase (ni timers ni
  sockets).
- `ServiceAction{unit, action}`: `START`, `STOP`, `RESTART`, `RELOAD`, `ENABLE`, `DISABLE`. El agente valida
  `^[A-Za-z0-9@._:\\-]{1,250}\.service$`, ejecuta `systemctl --no-ask-password <verbo> -- <unidad>` (60 s) y
  devuelve el estado nuevo (`systemctl show -p ActiveState,SubState,UnitFileState`) como JSON; luego refresca el
  inventario. **Rechaza** `STOP`, `RESTART` y `DISABLE` de `talaia-agent.service` (se cortaría a sí mismo; se
  actualiza por `AgentUpdate`).
- Core: la unidad debe estar en el último inventario; `confirm` obligatorio para `STOP`, `RESTART` y `DISABLE`.
  Auditoría `service_<acción>`.
- Panel: aviso reforzado («puedes perder el acceso») para `ssh.service`, `sshd.service`, `docker.service`,
  `networking.service`, `systemd-networkd.service`, `NetworkManager.service` (lista fija en el panel).

### D3 · journald
- `ReadJournal`: `unit` (vacío = todo; si no, una unidad del inventario), `max_priority` (0–7, `optional`;
  ausente = todas), `since`/`until` (RFC 3339), `search` (literal, ≤ 200 caracteres, sin distinguir mayúsculas:
  el agente lo escapa con `regexp.QuoteMeta` y lo pasa como `--grep=` con `--case-sensitive=false`), `boot`
  (todos / actual `-b 0` / anterior `-b -1`), `tail` (1–1000, 200 por defecto), `after_cursor` (seguir; charset
  `^[A-Za-z0-9=;_-]{1,512}$`).
- `journalctl --output=json --no-pager` con argumentos sueltos; `MESSAGE` puede venir como array de bytes. Salida
  JSON `{entries[{ts,unit,priority,pid,message}], cursor}`; mensaje recortado a 4 KiB y total a 256 KiB.
- Seguir = sondeo cada 3 s con `after_cursor` (como `LogsPanel`). Auditoría `journal_read` sin el texto.

### D4 · Terminal del host
- Mismo protocolo que la de contenedor: `TerminalOpen.target = HOST` (campo 6) y `container` vacío. El agente
  abre un PTY local (`github.com/creack/pty`, MIT; verificar versión y licencia y regenerar avisos), shell de la
  lista cerrada (`/bin/bash -l` o `/bin/sh -l`) **como root**, entorno limpio (`TERM`, `HOME=/root`, `PATH`
  estándar, `LANG` del sistema), cwd `/root`, `Setsid`; al cerrar se mata el grupo de procesos.
- Límites: los de §3.6 de `D-terminal`, con como máximo 2 sesiones de host por servidor.
- El agente la permite salvo `[terminal] host = false` en `agent.toml`; lo informa en `Inventory.host_terminal`.
  Agentes viejos informan `false` → el panel la muestra «no disponible, actualiza el agente».
- Core: `POST /api/v1/servers/{id}/terminal` exige sesión completa, `confirm` **y un código TOTP nuevo**
  (`code`, misma verificación sin reutilización que el cambio de contraseña). Grabación obligatoria con el mismo
  grabador; auditoría `host_terminal_open`/`host_terminal_close`.
- Panel: pestaña Terminal con franja roja permanente «root en <servidor>» y la marca de grabación.

### D5 · Pantallas
- Pantalla de servidor (maqueta 2 de `docs/design/talaia-roadmap.html`): pestañas **Resumen** (métricas,
  actualizaciones, reinicio necesario, servicios fallidos), **Contenedores/Stacks** (lo actual), **Servicios**,
  **Registros**, **Actualizaciones**, **Terminal**.
- Flota: insignias de actualizaciones (seguridad en rojo) y «reinicio necesario» por servidor.
- Auditoría: `GET /api/v1/audit?before=<id>&limit=<1..200>` y pantalla de solo lectura (filtros por acción y
  servidor) en Ajustes.

## Contrato (proto, no renumerar)

`CoreMsg`: `ServiceAction service_action = 23; ReadJournal read_journal = 24; AptStatus apt_status = 25;
AptUpgrade apt_upgrade = 26; HostReboot host_reboot = 27;`. `Inventory`: `AptSummary apt = 17; bool
host_terminal = 18;`. `SystemdUnit`: `string unit_file_state = 6;`. `TerminalOpen`: `TerminalTarget target = 6;`
(`UNSPECIFIED` = contenedor, por compatibilidad). Texto exacto en `proto/agent.proto` (paso 1).

## Pasos

Cada paso sube el parche (`scripts/bump-version.sh`) con su entrada en los dos CHANGELOG; el cierre sube a 0.4.0.
Ejecutor por defecto: AgentRelay. Marca ✅ al terminar.

| # | Paso | Archivos | Terminado cuando | Ejecutor · esfuerzo |
|---|---|---|---|---|
| 1 ✅ | Contrato: mensajes y campos de arriba, código generado | `proto/agent.proto`, `proto/gen/**` | `buf lint` y `buf generate` limpios; compilan core y agent | Opus (hecho) |
| 2 ✅ | Agente apt lectura: paquete `agent/internal/apt` (parseo de `apt-get -s`, retenidos, reinicio necesario, fecha de listas), `AptSummary` en el inventario (arranque, 6 h), trabajo `AptStatus` | `agent/internal/apt/**`, `agent/internal/jobs/apt.go`, `agent/internal/inventory/inventory.go` | pruebas con salidas reales de Debian 12/13 como fixtures (con y sin seguridad, paquete nuevo, retenido, kernel nuevo) | AgentRelay · alto |
| 3 ✅ | Core apt lectura: guardar `AptSummary` en `AgentInfo` y exponerlo en la API de servidores; `POST …/apt/status` (con `refresh`) y caducidades | `core/internal/hub/**`, `core/internal/api/apt.go` (+ registro de rutas) | pruebas de API; `refresh` cambia la caducidad | AgentRelay · medio |
| 4 ✅ | Panel: pestaña Actualizaciones (lista, seguridad primero, retenidos, fecha de listas, botón «Comprobar ahora») e insignias en Flota | `web/src/screens/server/UpdatesSection.tsx`, `web/src/screens/fleet/**`, locales | capturas claro/oscuro/móvil; `pnpm build lint test` e i18n verdes | AgentRelay · medio |
| 5 ✅ | Agente apt escritura: `AptUpgrade` en unidad transitoria con reanudación, `HostReboot` | `agent/internal/apt/**`, `agent/internal/jobs/apt.go` | pruebas con un `systemd-run` simulado (éxito, fallo, reanudación tras reinicio, bloqueo de apt); prueba real en el Debian | AgentRelay · alto |
| 6 ✅ | Core + panel de actualizar y reiniciar: endpoints con `confirm` (reinicio = nombre del servidor), progreso en vivo del log, avisos de Docker y retenidos | `core/internal/api/apt.go`, `web/src/screens/server/UpdatesSection.tsx`, locales | prueba de API (sin confirmar → 400); actualización real en el Debian desde el panel | AgentRelay · alto |
| 7 ✅ | systemd en el agente: `unit_file_state` y `ServiceAction` | `agent/internal/inventory/systemd.go`, `agent/internal/jobs/service.go` | pruebas con salidas fijas; `talaia-agent.service` rechazado | AgentRelay · medio |
| 8 ✅ | systemd en core + panel: API con `confirm`, pestaña Servicios (filtro, estado, acciones, avisos de acceso) | `core/internal/api/services.go`, `web/src/screens/server/ServicesSection.tsx`, locales | pruebas de API; capturas | AgentRelay · medio |
| 9 ✅ | journald en el agente: `ReadJournal` | `agent/internal/jobs/journal.go` | pruebas: escape de `search`, cursor inválido rechazado, `MESSAGE` en bytes, recortes | AgentRelay · medio |
| 10 | journald en core + panel: API y pestaña Registros con filtros y seguir | `core/internal/api/journal.go`, `web/src/screens/server/JournalSection.tsx`, locales | pruebas; capturas | AgentRelay · medio |
| 11 | Terminal del host en el agente (PTY, límites, opción `host`) | `agent/internal/terminal/host.go`, `agent/internal/config/**` | pruebas: shell fuera de lista, límite de 2, cierre mata el grupo; prueba real | AgentRelay · alto |
| 12 | Terminal del host en core + panel (TOTP al abrir, grabación, franja roja) | `core/internal/api/terminal*.go`, `core/internal/terminal/**`, `web/src/screens/server/TerminalPanel.tsx` | pruebas: sin código o código reutilizado → rechazo; grabación exportable | AgentRelay · alto |
| 13 | Pantalla de servidor con pestañas y Resumen (maqueta 2) y pantalla de auditoría | `web/src/screens/ServerScreen.tsx`, `web/src/screens/settings/**`, `core/internal/api/audit.go` | capturas; pruebas | AgentRelay · medio |
| 14 | Documentación EN+ES (`docs/*/` servidor, seguridad), **revisión de seguridad de la fase (Opus)**, semana de prueba real y cierre 0.4.0 | `docs/**`, ROADMAP, TODO | criterio de la fase cumplido con el mantenedor | Opus + mantenedor |

## Cómo seguir

`/clear` → `/model sonnet` → «ejecuta el paso 2 de `.agents/plans/fase3/fase3.PLAN.md`» (orquestar con AgentRelay y
revisar). Pasos en orden; 7–10 pueden ir en paralelo con 5–6 si los archivos no se cruzan. El paso 14 vuelve a
Opus. Ante un imprevisto de diseño, el ejecutor para y lo anota aquí.
