# Diseño de la terminal en contenedor (borrador)

Estado: **borrador de Sonnet, sin implementar, para que Opus lo revise antes de escribir una línea.** La terminal es la parte de la fase 2 con más riesgo: da a una persona acceso interactivo a un contenedor, y el agente corre como root.

Reglas del proyecto que mandan (CLAUDE.md §3, `docs/en/architecture.md` §3.2): comandos tipados salvo las terminales que el usuario abre explícitamente y que quedan grabadas; nada destructivo sin confirmación; permisos comprobados en el core.

## 1. Qué es

Una sesión interactiva `docker exec -it <contenedor> <shell>` con PTY, que el usuario abre desde el panel (xterm.js) y que viaja: navegador ⇄ core ⇄ agente. Sin SSH, sin abrir puertos nuevos.

## 2. Propuesta

**Transporte.** WebSocket navegador ⇄ core (misma sesión y cookie que la API, comprobación de `Origin`). Core ⇄ agente: el stream gRPC existente con mensajes nuevos `TerminalOpen`/`TerminalData`/`TerminalResize`/`TerminalClose` (flujo bidireccional multiplexado por `session_id`), **no** una conexión nueva.

**Apertura.** `POST /api/v1/servers/{id}/containers/{c}/terminal` (sesión completa, contenedor informado por el agente, **confirmación explícita**) crea la sesión y devuelve un `ticket` de un solo uso y 30 s para abrir el WebSocket. El ticket evita pasar la cookie de sesión como autorización del WebSocket y ata la sesión al usuario.

**Ejecución en el agente.** API de Docker `exec` (create + start con `Tty: true`, `AttachStdin`), **comando fijo** (`/bin/sh`, probando `bash` si existe), **usuario del contenedor** (no root del host salvo que el contenedor lo sea), sin `Privileged`, sin variables del agente. Redimensionado por `exec/{id}/resize`.

**Límites.** Una sesión por usuario y contenedor; máximo N sesiones por servidor; **caducidad por inactividad** (p. ej. 15 min) y duración máxima (p. ej. 4 h); tamaño máximo por mensaje y control de flujo para no llenar memoria; se cierra si el agente se desconecta o la sesión del panel se revoca.

**Grabación y auditoría** (la fase 4 la exige; conviene diseñarla ya). Auditoría: `terminal_open` y `terminal_close` con usuario, servidor, contenedor, duración y motivo de cierre. Grabación: salida del PTY en formato asciicast guardada en el core, **cifrada con la clave de secretos**, con retención configurable. La entrada del teclado **no** se graba (contraseñas), pero sí el eco del terminal, así que un secreto escrito con eco queda grabado: avisarlo en la interfaz.

## 3. Decisiones que necesito de la revisión

1. **¿Terminal de contenedor solo, o también del host?** El host es mucho más delicado (root). Propuesta: **solo contenedores** en la fase 2; la terminal del host va en la fase 3 con su propio diseño.
2. **¿Se permite la terminal en contenedores `privileged` o con el socket de Docker montado?** Equivale a root del host. Propuesta: permitirla pero **avisar** con un banner y registrarlo.
3. **Grabación obligatoria u opcional.** Propuesta: obligatoria; sin grabación no se abre.
4. **Forma del ticket y del WebSocket** (CSRF en WebSocket, `Origin`, expiración, un solo uso): confirmar que no queda un camino de robo de sesión.
5. **Aislamiento en el agente:** una sesión no debe poder leer el stream de otra; `session_id` aleatorio de 128 bits y comprobado contra el usuario en el core.
6. **Denegación de servicio:** tope de memoria por sesión y de sesiones simultáneas; qué pasa si el navegador no lee (contrapresión).

## 4. Pasos de implementación (cuando se apruebe)

1. Proto: mensajes de terminal. 2. Agente: `exec` con PTY, redimensionado, límites. 3. Core: tickets, WebSocket, enrutado de sesiones, auditoría, grabación cifrada. 4. Panel: xterm.js (autoalojado, la CSP solo permite el propio origen), aviso de grabación, confirmación. 5. Pruebas con un Docker real y revisión de seguridad final con Opus.
