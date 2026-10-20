# Diseño de los trabajos y de la gestión de Docker (fase 2)

Estado: **revisado por Opus el 2026-10-20 (decisiones en §6).** Escrito a posteriori por Sonnet. Describe lo que ya está construido (commits locales de la fase 2) para que la revisión se haga sobre hechos, no sobre intenciones. Cada punto indica dónde mirar y qué se pide revisar.

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

## 6. Revisión de seguridad (Opus, 2026-10-20) — decisiones

Estado: **revisado.** Lo construido es sólido en lo esencial (órdenes tipadas, regex antes de construir rutas, sin shell, sin `force`, `serverID` tomado de la conexión mTLS y no del mensaje). Respuestas a los «Revisar»:

- **§1 Aislamiento entre servidores:** correcto. El hub pasa el `serverID` de la conexión autenticada y `UpdateJobState` filtra por él; un agente comprometido solo puede mentir sobre sus propios trabajos. `output` acotado a 256 KiB en el core y 64 KiB / 256 KiB (compose / logs) en el agente.
- **§3 `confirm` y listas cerradas:** correcto; no hay otra ruta que llegue a `remove`/`down`. La auditoría guarda nombres, ids y hashes, nunca contenido. `Cache-Control: no-store` lo pone el middleware a **todo** `/api/` (§4.4 resuelto).
- **§4.1 Un compose es código con privilegios — DECIDIDO: se asume.** En la fase 2 hay un único rol (administrador con contraseña + 2FA) y ese rol **equivale a root en cada servidor gestionado** (también por la terminal y la adopción). No se filtra el compose: una lista de campos peligrosos daría falsa seguridad (hay demasiadas vías: `volumes`, `devices`, `cap_add`, `pid: host`, `network_mode: host`, `security_opt`, `build`…). Obligaciones: (a) decirlo en la documentación de seguridad (EN/ES) y con una nota fija en el editor de stacks (paso 9); (b) en la **fase 4**, los permisos «editar stacks», «adoptar» y «terminal» se marcan como *equivalentes a root* y no se conceden por defecto.
- **§4.2 `adopted.json` y rutas:** el registro vive en un directorio de root y solo lo escribe el agente: aceptable. El riesgo real estaba en el **directorio del proyecto adoptado**, que puede ser de un usuario sin privilegios. **Corregido:** `replaceFile` usa un temporal exclusivo con nombre aleatorio (antes `ruta.new` con `WriteFile`, que seguía enlaces: un usuario local podía hacer que root machacara cualquier fichero); `Read` rechaza enlaces sin carrera (`Lstat` + `SameFile`) y acota el tamaño; `Adopt` rechaza un compose que sea enlace y ficheros o directorio escribibles por grupo/otros. Queda asumido: el **dueño** del directorio adoptado puede cambiar lo que root ejecuta en el siguiente `up` (igual que antes de Talaia).
- **§4.3 `docker compose config`:** solo interpola; no ejecuta ni construye. Puede leer ficheros del host (`env_file`, `include`) y mostrar errores con su contenido, pero solo al administrador, que ya es root (§4.1). `--env-file` siempre apunta al directorio de preparación. **Corregido:** al validar un adoptado con `.env` vacío se usaba el `.env` antiguo.
- **Fallos encontrados fuera de la lista (corregidos):** (1) el core cerraba como fallido **cualquier** trabajo a los 2 min, y un `pull`/`up` tiene 10 min en el agente → ahora la caducidad depende del tipo (`jobTimeout(kind)`, siempre mayor que la del agente). (2) Cortes por bytes que parten un carácter: protobuf no serializa cadenas UTF-8 inválidas y PostgreSQL las rechaza → el trabajo no terminaba nunca; ahora se sanea en el agente (`execute`) y en el core. (3) Un stack nuevo con el nombre de un proyecto en marcha no gestionado lo absorbía y `up --remove-orphans` borraba sus contenedores sin confirmación → `409 name_taken`; ese proyecto se adopta.

Pendiente menor (no bloquea): copia del compose anterior al editar un adoptado (hoy se pierde la versión previa); retención de la tabla `job`; límite de trabajos simultáneos por agente (hoy lo acota la cola de 32 del core).

## 7. Pendiente fuera de este diseño

Terminal en contenedor (borrador en `D-terminal.DESIGN.md`), seguimiento de logs en directo (`-f`), permisos por usuario (fase 4), retención y limpieza de la tabla `job`.
