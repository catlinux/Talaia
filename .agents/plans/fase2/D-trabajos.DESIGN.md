# Diseño de los trabajos y de la gestión de Docker (fase 2)

Estado: **borrador escrito a posteriori por Sonnet, pendiente de revisión de seguridad con Opus.** Describe lo que ya está construido (commits locales de la fase 2) para que la revisión se haga sobre hechos, no sobre intenciones. Cada punto indica dónde mirar y qué se pide revisar.

## 1. Modelo

Un **trabajo** es una orden tipada que el core manda al agente de un servidor por el stream gRPC que el agente abrió hacia fuera. El agente la ejecuta y va diciendo cómo le va. No existe ninguna orden «ejecuta este texto».

```
panel ──HTTP (sesión completa + CSRF/Origin)──▶ core ──CoreMsg{job_id, orden tipada}──▶ agente
panel ◀──GET /api/v1/jobs/{id}──────────────── core ◀──AgentMsg{JobEvent}────────────── agente
```

- Contrato: `proto/agent.proto`. Órdenes (`CoreMsg.cmd`): `ContainerAction` (10), `ReadLogs` (11), `StackWrite` (12), `StackAction` (13), `StackRead` (14), `StackAdopt` (15), `ListResources` (16), `RemoveResources` (17). Respuestas: `JobEvent` (campo 3 de `AgentMsg`) con estado, mensaje corto y `output` acotado. Telemetría aparte: `ContainerStats` (5).
- Tabla `job` (migración 0005 y 0006): `server_id`, `kind`, `params`, `state`, `message`, `output`, `created_by`, marcas de tiempo. **El estado solo avanza** (`pending → running → succeeded|failed`) y un trabajo terminado no vuelve a cambiar. Un evento solo actualiza un trabajo **del servidor que lo envía** (`UpdateJobState` filtra por `server_id`).
- Idempotencia: el agente recuerda los últimos 256 `job_id` y responde con el primer resultado si le repiten uno (sin repetir el efecto ni guardar la salida).
- Caducidad: el core cierra como fallido un trabajo sin terminar a los 2 minutos al leerlo (`FailStaleJob`). No hay reintentos.

**Revisar:** que ningún camino permita a un agente tocar trabajos de otro servidor; que el tamaño de `output` (256 KiB en el core, 64 KiB de salida de compose) acote el uso de memoria; qué pasa si un agente comprometido miente en sus eventos (hoy: solo afecta a los trabajos de su propio servidor).

## 2. Cola y envío (core)

`hub.Dispatch(serverID, msg)` mete la orden en la cola de la conexión del agente (32 mensajes, no bloquea: si está llena devuelve `agent_busy`). **Un único goroutine escribe** en el stream (acks de latido y órdenes salen por la misma cola). Si el agente no está conectado: `agent_offline` (409) y el trabajo se cierra como fallido, no queda pendiente.

## 3. Qué exige cada endpoint (core)

Todos detrás de sesión completa (contraseña + 2FA), con la protección de origen/CSRF de la API. Ninguno acepta rutas del cliente.

| Endpoint | Validación | Confirmación | Auditoría |
|---|---|---|---|
| `POST …/containers/{c}/actions` | referencia de contenedor por regex **y que el agente la haya informado**; acción de lista cerrada | `remove` exige `confirm` | `container_<acción>` |
| `POST …/containers/{c}/logs` | igual; `tail` 0–1000 | — | `container_logs` (nunca el texto) |
| `PUT …/stacks/{n}` | nombre `^[a-z0-9][a-z0-9_-]{0,62}$`; compose ≤ 256 KiB, env ≤ 64 KiB | — | `stack_write` con **hashes** de los ficheros |
| `POST …/stacks/{n}/read` | stack gestionado | — | `stack_read` (el `.env` puede tener secretos) |
| `POST …/stacks/{n}/actions` | acción de lista cerrada; stack gestionado | `down`, `remove` exigen `confirm`; `remove` de un adoptado se rechaza; `forget` solo adoptados | `stack_<acción>` |
| `POST …/stacks/{n}/adopt` | el proyecto debe estar entre los que **el agente informó**; el core manda solo el nombre | — | `stack_adopt` |
| `POST …/resources/list` | tipo de lista cerrada | — | `resources_list` |
| `POST …/resources/remove` | ≤ 200 ids, cada uno por regex de su tipo | `confirm` obligatorio | `resources_remove` con lo pedido |

**Revisar:** que `confirm` no se pueda saltar desde otra ruta; que las listas cerradas estén completas; que la auditoría no filtre secretos (hoy: hashes, nombres e ids; nunca contenido de ficheros ni logs).

## 4. Ejecución en el agente

El agente corre como **root** y sin sandbox de systemd (necesita Docker, `/opt` y `/etc`). Por eso toda la superficie está en `agent/internal/jobs` y `agent/internal/stacks`:

- **Docker por el socket** con la biblioteca estándar (sin SDK): `start/stop/restart` y `DELETE` sin `force` ni volúmenes; el borrado de imágenes, volúmenes y redes se hace **uno a uno sin `force`** para que Docker rechace lo que está en uso. Referencias validadas por regex (`ValidContainerRef`, `ValidResourceRef`) **antes** de construir la ruta, y escapadas.
- **`docker compose`** se ejecuta con `exec.CommandContext` con **argumentos fijos** (`compose --project-name <n> …`), directorio de trabajo fijo y **entorno reducido** (`PATH`, `HOME`). Nunca pasa por un shell.
- **Stacks gestionados** en `/opt/talaia/stacks/<n>/` (`compose.yaml` 0644, `.env` 0600). `Write` valida con `docker compose config -q` en un directorio de preparación **antes** de reemplazar nada y escribe con `.new` + `rename` (un cambio roto nunca sustituye uno que funciona). Se rechazan **enlaces simbólicos** donde debería haber un directorio de stack.
- **Adopción:** el agente localiza el proyecto por las etiquetas de sus contenedores, exige **un solo fichero compose con rutas absolutas** que existan, y guarda `adopted.json`. No copia ni mueve nada; compose se ejecuta con `--project-directory` y `-f` originales. «Eliminar» un adoptado se rechaza; «dejar de gestionar» borra solo el registro. Editar un adoptado reescribe el fichero original conservando su modo.
- Tiempos: 90 s (contenedores), 2 min (escribir stack), 10 min (operar stack), 5 min (recursos).

**Revisar (lo más importante):**
1. **Un compose es código con privilegios**: puede montar `/`, usar `privileged: true`, exponer el socket de Docker. Hoy no se filtra nada. Decidir si se advierte, se bloquea (lista de campos peligrosos), o se asume que el administrador con 2FA es de confianza. Recomendación provisional: asumirlo, mostrarlo claro en la documentación y en la interfaz.
2. `adopted.json` lo escribe el agente pero vive en un directorio de root: comprobar que no se pueda usar para hacer que compose lea ficheros fuera de lo previsto (el core nunca envía rutas, pero el registro sí contiene rutas absolutas).
3. La validación de `Write` ejecuta `docker compose config` sobre contenido del usuario: comprobar que `config` no ejecuta nada (no lo hace, solo interpola), y que `--env-file` no se puede dirigir fuera.
4. `ReadLogs` y `StackRead` devuelven texto potencialmente sensible al navegador: confirmar que `Cache-Control: no-store` cubre los endpoints de trabajo.

## 5. Datos que llegan al navegador

Listas (contenedores, stacks, recursos) salen del inventario que el agente informa; la salida de trabajos se muestra tal cual (los mensajes de error de Docker pueden contener texto del usuario). El panel renderiza todo como texto (React), sin `dangerouslySetInnerHTML`.

## 6. Pendiente fuera de este diseño

Terminal en contenedor (borrador en `D-terminal.DESIGN.md`), seguimiento de logs en directo (`-f`), permisos por usuario (fase 4), retención y limpieza de la tabla `job`.
