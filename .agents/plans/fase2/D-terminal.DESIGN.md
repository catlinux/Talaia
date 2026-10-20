# Diseño de la terminal en contenedor

Estado: **aprobado por Opus el 2026-10-20 (§3 y §4), sin implementar.** Borrador original de Sonnet (§1–§2). La terminal es la parte de la fase 2 con más riesgo: da a una persona acceso interactivo a un contenedor, y el agente corre como root.

Reglas del proyecto que mandan (CLAUDE.md §3, `docs/en/architecture.md` §3.2): comandos tipados salvo las terminales que el usuario abre explícitamente y que quedan grabadas; nada destructivo sin confirmación; permisos comprobados en el core.

## 1. Qué es

Una sesión interactiva `docker exec -it <contenedor> <shell>` con PTY, que el usuario abre desde el panel (xterm.js) y que viaja: navegador ⇄ core ⇄ agente. Sin SSH, sin abrir puertos nuevos.

## 2. Propuesta original (resumen)

WebSocket navegador ⇄ core; mensajes de terminal multiplexados por el stream gRPC existente; apertura por `POST` con confirmación que devuelve un ticket; `exec` por la API de Docker con comando fijo; límites; auditoría y grabación cifrada. Lo que sigue la concreta y la corrige donde hace falta.

## 3. Decisiones

1. **Solo contenedores en la fase 2.** La terminal del host es de la fase 3, con su propio diseño.
2. **Contenedores `privileged`, con `pid: host` o con el socket de Docker montado: se permite**, con un aviso visible antes de abrir («esta terminal equivale a root del servidor») y `privileged: true` en la auditoría. Motivo: en la fase 2 el administrador ya es root en el servidor (D-trabajos §6); bloquearlo no protege nada. El agente lo detecta con `GET /containers/{id}/json` (`HostConfig.Privileged`, `HostConfig.PidMode == "host"`, algún `Mounts[].Source` igual a `/var/run/docker.sock` o `/run/docker.sock`) y lo devuelve al abrir.
3. **Grabación obligatoria.** Si no se puede grabar (base de datos caída, tope alcanzado), la sesión no se abre o se cierra. Se graba la **salida** del PTY (asciicast v2, eventos `o`) y los redimensionados; **la entrada no**. Aviso permanente en la interfaz: «la sesión se graba; lo que escribas con eco queda en la grabación».
4. **Ticket y WebSocket** (cerrado, sin camino de robo de sesión):
   - `POST /api/v1/servers/{id}/containers/{c}/terminal` con `{"confirm": true, "shell": "sh"|"bash", "cols", "rows"}`, sesión completa, contenedor informado por el agente. Devuelve `{"session_id", "ticket", "privileged"}`.
   - Ticket: 32 bytes aleatorios, en memoria del core guardado **como SHA-256**, un solo uso, caduca a los **30 s**, atado al **id de la sesión del panel** (no solo al usuario) y al `session_id` de terminal.
   - `GET /api/v1/terminal/{session_id}/ws` (upgrade). Exige **las tres cosas**: cookie de sesión válida y completa **de la misma sesión** que pidió el ticket, `Origin` presente y en la lista de orígenes permitidos (sin el atajo de `Sec-Fetch-Site`), y el ticket en el **primer mensaje** del WebSocket (no en la URL: acabaría en registros de proxies). Primer mensaje distinto, ticket gastado o caducado → cierre 4401 sin más.
   - Cada 60 s y al cerrar sesión, el core comprueba que la sesión del panel sigue viva; si se revocó o caducó, cierra la terminal (`session_revoked`).
5. **Aislamiento:** el `session_id` (UUID v4, 122 bits aleatorios) lo genera **el core**. El core enruta por la pareja `(server_id de la conexión mTLS, session_id)`: un agente solo puede alimentar sesiones de su propio servidor, y un WebSocket solo recibe su sesión. El agente rechaza `TerminalInput`/`Resize`/`Close` de un `session_id` que no abrió.
6. **Límites y contrapresión:**
   - Por servidor: máx. **4** sesiones; por usuario y contenedor: **1**. Inactividad (ni entrada ni salida): **15 min**. Duración máxima: **4 h**. Grabación máxima por sesión: **50 MiB** (al llegar, se cierra con `recording_limit`).
   - Entrada: mensaje del navegador ≤ **4 KiB**; el core la manda con un envío **que espera** (nuevo `hub.DispatchWait(ctx, serverID, msg)` con 5 s de plazo) en lugar del `Dispatch` que falla con la cola llena; si vence, cierra la sesión (`agent_busy`). Así la contrapresión llega al navegador por TCP y no se pierden teclas.
   - Salida: el agente lee el PTY en trozos de ≤ **16 KiB** y limita cada sesión a **1 MiB/s**; si su cola de envío está llena, deja de leer el PTY (el proceso del contenedor se bloquea, no la memoria del agente). El core guarda por sesión como máximo **1 MiB** pendiente hacia el navegador; si se supera, cierra (`slow_client`).
7. **Ejecución en el agente:** API de Docker `POST /containers/{id}/exec` con `Cmd` de **lista cerrada** (`["/bin/sh"]` o `["/bin/bash"]`, elegida en la interfaz; nunca texto libre ni `-c`), `Tty: true`, `AttachStdin/Stdout/Stderr: true`, **sin** `User` (el del contenedor), **sin** `Privileged`, `Env: ["TERM=xterm-256color"]` y nada del entorno del agente. Conexión con *hijack* (`POST /exec/{id}/start` con `Upgrade: tcp`), redimensionado con `POST /exec/{id}/resize`. Al cerrar: se cierra la conexión hijacked y, si el proceso sigue vivo tras 2 s, no se mata nada más (el `exec` muere al perder su TTY).
8. **Auditoría:** `terminal_open` (usuario, servidor, contenedor, shell, `privileged`) y `terminal_close` (duración, bytes grabados, motivo: `user`, `idle`, `max_duration`, `recording_limit`, `slow_client`, `agent_offline`, `session_revoked`, `exited`). Nunca contenido.
9. **Grabación:** tabla `terminal_session` (id, server_id, container, shell, created_by, privileged, opened_at, closed_at, close_reason, recorded_bytes) y `terminal_chunk` (session_id, seq, data cifrado). Cada trozo (≤ 64 KiB de asciicast) se cifra **por separado** con el `SecretBox` existente (`core/internal/auth/secretbox.go`) usando el `session_id` y `seq` como datos asociados. Retención configurable `terminal_recording_days` (por defecto **90**), purga al arrancar y cada 24 h. Reproductor en el panel: **fase 4**. En la fase 2, `talaia-core terminal list` y `talaia-core terminal export <id>` (descifra y escribe el `.cast` por la salida estándar) para que la grabación sea usable.

## 4. Contrato (proto, nunca renumerar)

`CoreMsg` (los mensajes de terminal **no** llevan `job_id`; no son trabajos):
- `TerminalOpen terminal_open = 18;` → `session_id`, `container`, `TerminalShell shell` (`UNSPECIFIED=0, SH=1, BASH=2`), `cols`, `rows`.
- `TerminalInput terminal_input = 19;` → `session_id`, `bytes data` (≤ 4 KiB).
- `TerminalResize terminal_resize = 20;` → `session_id`, `cols`, `rows` (1–1000).
- `TerminalClose terminal_close = 21;` → `session_id`.

`AgentMsg`:
- `TerminalOpened terminal_opened = 6;` → `session_id`, `bool privileged`, `string error` (vacío si abrió).
- `TerminalOutput terminal_output = 7;` → `session_id`, `bytes data` (≤ 16 KiB).
- `TerminalClosed terminal_closed = 8;` → `session_id`, `string reason`, `int32 exit_code`.

Los datos van en `bytes`, no en `string`: la salida de un PTY no tiene por qué ser UTF-8.

## 5. Pasos de implementación

Cada paso con su commit, sus pruebas y las entradas de CHANGELOG EN/ES. Ejecutor: Sonnet (esfuerzo alto) por AgentRelay, salvo donde se indica.

1. ✅ **Proto** (§4) + `buf generate`. Hecho cuando `proto/gen` está al día y compila todo.
2. **Agente** `agent/internal/terminal/`: sesiones, `exec` con hijack, redimensionado, límites de §3.6, detección de §3.2, cierre al perder el stream. Pruebas con un servidor HTTP simulado que hace hijack (como las de `jobs`). Hecho cuando las pruebas cubren: shell fuera de lista rechazado, `session_id` desconocido ignorado, límite de sesiones, inactividad, contrapresión.
3. **Core, hub y almacenamiento:** `DispatchWait`, enrutado `(server_id, session_id)`, migración de `terminal_session`/`terminal_chunk`, grabación cifrada, retención, órdenes `terminal list|export`. Pruebas con PostgreSQL.
4. **Core, API y WebSocket:** endpoint de apertura, ticket (§3.4), WebSocket (biblioteca `github.com/coder/websocket`, versión estable actual fijada en `go.mod`), comprobación periódica de la sesión, auditoría. Pruebas: ticket reutilizado, caducado, de otra sesión, `Origin` ausente o ajeno, sin cookie, sesión revocada a mitad.
5. **Panel:** `@xterm/xterm` y `@xterm/addon-fit` empaquetados (la CSP es `default-src 'self'`; nada de CDN), diálogo de confirmación con elección de shell, aviso de grabación y aviso de *privileged*, motivo de cierre traducido. Textos ca/es/en. Capturas claro/oscuro/móvil.
6. **Documentación EN/ES** (seguridad: el administrador equivale a root; grabación y retención; `terminal export`) y **prueba con un Docker real** en el Debian del mantenedor.
7. **Revisión de seguridad final con Opus** del diff completo antes de dar el paso 8 de la fase por bueno.
