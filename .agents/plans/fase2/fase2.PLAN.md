# Fase 2 · Contenedores y stacks (0.3.0)

Estado: **solo plan, sin código.** La regla del proyecto es no adelantar fases sin permiso del mantenedor; esto deja la hoja de ruta lista para cuando se abra la fase (tras cerrar la 1 con un VPS real). Criterio de la fase, del ROADMAP: *«puedes dejar de usar Portainer para todo lo que haces hoy»*.

Punto de partida (ya hecho en la fase 1): el agente conecta por mTLS, manda inventario (contenedores, proyectos Compose, servicios) y métricas; el core tiene sesión con 2FA, auditoría y el botón «Añadir servidor». El contrato `proto/agent.proto` reserva los números ≥ 10 de `CoreMsg` para comandos tipados y el campo 3 de `AgentMsg` para `JobEvent`.

Reglas que mandan en toda la fase (CLAUDE.md §3): comandos **tipados**, nunca texto libre ejecutado en el host; nada destructivo sin tarea explícita y **confirmación en la interfaz**; permisos comprobados en el core; todo texto por i18n (ca/es/en); documentación EN+ES; estilo visual del panel según `feedback-estilo-visual`.

## Decisiones de diseño (las toma un modelo potente, antes de implementar)

| # | Decisión | Opciones a valorar |
|---|---|---|
| D1 | Protocolo de trabajos | `job_id` idempotente, `JobEvent` (progreso, salida, resultado), reintentos y qué pasa si el agente se desconecta a mitad; tabla `job` |
| D2 | Acceso a Docker en el agente | API del socket Docker (cliente Go oficial) frente a invocar `docker`/`docker compose`; **recomendado** el cliente Go para contenedores/imágenes y `docker compose` solo para stacks |
| D3 | Registros y estadísticas en vivo | Flujos sobre el mismo canal gRPC (agente→core) y SSE/WebSocket core→navegador; límites de tamaño y de clientes |
| D4 | Terminal en contenedor | `exec` con PTY por el túnel, grabación de sesión (la fase 4 la exige), tiempo máximo, permiso aparte |
| D5 | Stacks | Directorio `/opt/talaia/stacks/<nombre>/` (compose.yaml + .env con permisos 0600), validación con `docker compose config`, secretos fuera del registro |
| D6 | Adopción | Cómo pasar a Talaia un proyecto Compose existente sin recrearlo (copiar o enlazar archivos, conservar el nombre del proyecto) |

Entregable: `.agents/plans/fase2/D-trabajos.DESIGN.md`, revisado por el mantenedor antes de seguir.

## Avance (2026-10-20, por petición «sigue con el desarrollo»)

Hecho como primer corte vertical, **sin pasar por la revisión de diseño D1–D6** (decisiones tomadas: cliente propio del socket Docker con la biblioteca estándar, `JobEvent` con estados que solo avanzan, comandos por una cola por agente con un único escritor al stream, el core exige `confirm` para eliminar y solo actúa sobre contenedores que el agente informó): pasos 1 (contrato, cola e historial de trabajos), 2 (acciones de contenedor en el agente), 3 (API con permisos y confirmación) y 4 en parte (pantalla de servidor con acciones). Hecho después (paso 5, primer corte): stacks Compose gestionados en `/opt/talaia/stacks` (escribir con validación, leer, up/down/pull/restart/remove, sección y editor en el panel). Hecho también el paso 6 (adopción de proyectos existentes sin copiar ni reiniciar: el stack apunta a su directorio original y «dejar de gestionar» solo borra la anotación). Falta de la fase: imágenes/volúmenes/redes y limpieza (7), terminal (8), pantalla de stack y cierre (9). Hecho también: lectura de registros bajo demanda (`ReadLogs`, salida en el trabajo, visor en el panel). Falta de ellos: seguimiento de registros en directo (`-f`), estadísticas en vivo, y revisión de seguridad (Opus) del diseño antes de dar la fase por buena.

## Pasos

| # | Paso | Archivos | Terminado cuando | Ejecutor |
|---|---|---|---|---|
| 1 | Contrato y plomería de trabajos: mensajes tipados, `JobEvent`, cola e historial en el core, auditoría | `proto/**`, `core/internal/jobs/**`, `core/internal/store/**` | pruebas de ida y vuelta con un comando de prueba; reinicio del core sin perder trabajos | alto |
| 2 | Agente: contenedores (listar, detalle, iniciar, parar, reiniciar, eliminar, registros, estadísticas) | `agent/internal/docker/**` | pruebas con un Docker real en el Debian de pruebas | medio |
| 3 | API del core de contenedores con permisos y confirmación de acciones destructivas; flujos de registros | `core/internal/api/**` | pruebas de API; acción sin confirmar rechazada | medio |
| 4 | Panel: pantalla de servidor con pestaña de contenedores, acciones con diálogo de confirmación, visor de registros | `web/src/screens/server/**`, `web/src/locales/*/` | capturas en claro/oscuro/móvil; `pnpm build/lint/test` e i18n en verde | medio + revisión de diseño |
| 5 | Stacks Compose: crear, editar compose/.env con validación, `up`/`down`/`pull`/`restart` | `agent/internal/stacks/**`, `core/**`, `web/**` | un stack de ejemplo se despliega y se actualiza desde el panel | alto |
| 6 | Adoptar contenedores y proyectos existentes | `agent/internal/stacks/**`, `web/**` | los proyectos del Debian de casa aparecen como stacks de Talaia sin recrearlos | alto |
| 7 | Imágenes, volúmenes, redes y limpieza (con vista previa de lo que se borra) | `agent/internal/docker/**`, `web/**` | limpieza con confirmación y resumen | medio |
| 8 | Terminal en contenedor | `agent/internal/terminal/**`, `core/**`, `web/**` | terminal funcional, con permiso y registro; **revisión de seguridad con Opus** | alto + Opus |
| 9 | Pantalla de stack (maqueta 3), documentación EN+ES y cierre de fase | `docs/**`, `web/**` | criterio de la fase cumplido en el Debian de casa; versión 0.3.0 si el mantenedor lo pide | medio |

## Cómo retomarlo

`/clear` → `/model opus` → «haz el diseño D1–D6 de `fase2.PLAN.md` y escribe `D-trabajos.DESIGN.md`». Después, con el diseño aprobado, `/model sonnet` y «ejecuta el paso 1». El ejecutor marca cada paso hecho aquí y, ante un imprevisto de diseño, para y lo anota en lugar de improvisar.
