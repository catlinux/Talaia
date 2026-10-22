# Fase 4 · Usuarios, permisos y auditoría (0.5.0)

Estado: **diseño de Opus (2026-10-22), sin código.** Criterio del ROADMAP: *«un usuario con acceso a un solo stack
no puede ver ni tocar nada más, comprobado con pruebas automáticas»*.

Punto de partida: un solo usuario (`app_user`) con contraseña + TOTP obligatorio, passkeys, PIN de dispositivo de
confianza, sesiones (`session`, etapas `mfa_pending`/`full`), auditoría (`audit_event.actor` = nombre de usuario,
pantalla hecha), trabajos con `job.created_by` (nombre de usuario), terminal de contenedor y de host **ya
grabadas** (fase 2/3; exportables solo con `talaia-core terminal export`). La tabla `organization` existe pero no se
usa. `talaiactl` solo imprime la versión. Rutas en `core/internal/api/api.go` (`NewRouter`); modelo de roles ya
descrito en `docs/*/architecture.md` §5.

Reglas de toda la fase (CLAUDE.md §3): **los permisos se comprueban siempre en el core** (el panel solo esconde);
textos por i18n ca/es/en; docs EN+ES; estilo según la memoria `feedback-estilo-visual`; una dependencia nueva obliga
a `node scripts/third-party-notices.mjs` y a actualizar `CREDITS.md`/`CREDITS.es.md` (recordar al mantenedor el
script de licencias para archivos nuevos).

## Decisiones

### D1 · Modelo
- **Una sola organización** (la fila de `organization` que crea la migración si no hay ninguna). Multi-organización
  queda fuera.
- **Grupos de servidores**: `server_group(id, name UNIQUE)`, `server.group_id NULL` (un servidor en 0 o 1 grupo).
- **Equipos**: `team(id, name UNIQUE)`, `team_member(team_id, user_id)`.
- **Asignaciones**: `role_binding(id, subject_type user|team, subject_id, scope_type, scope_server_id, scope_group_id,
  scope_name, role, created_by, created_at)`. `scope_type` ∈ `organization | group | server | stack | container`;
  stack y contenedor se identifican por `(server_id, nombre)` porque no tienen fila propia. CHECK de coherencia por
  tipo. Índice único para no duplicar.
- **Usuarios**: `app_user` gana `display_name`, `email` (único, nullable, en minúsculas), `disabled_at`. El usuario que
  ya existe recibe en la migración un `role_binding` `admin` sobre la organización.
- **Invariante**: siempre queda al menos un usuario activo con `admin` de organización (directo o por equipo). El core
  rechaza quitar, desactivar o degradar al último (`409 last_admin`).
- Desactivar un usuario revoca en la misma transacción sus sesiones, dispositivos de confianza y claves de API.

### D2 · Permisos y roles (paquete nuevo `core/internal/rbac`, puro, sin BD)
Permisos (constantes tipadas):

| Nivel mínimo del ámbito | Permisos |
|---|---|
| contenedor | `container.view`, `container.logs`, `container.control` (start/stop/restart), `container.terminal` |
| stack | `stack.view`, `stack.deploy` (up/down/restart/pull de lo que ya hay), `stack.edit` (escribir compose, adoptar), `container.remove` |
| servidor | `server.view`, `resources.manage`, `host.services.view`, `host.services.control`, `host.journal`, `host.apt.view`, `host.apt.upgrade`, `host.reboot`, `host.terminal`, `server.agent_update` |
| organización | `server.remove`, `server.enroll`, `users.manage`, `settings.manage`, `audit.view`, `recordings.view` |

Roles fijos (no hay roles personalizados en esta fase):

| Rol | Permisos |
|---|---|
| `admin` | todos |
| `server_operator` | todo lo de contenedor, stack y servidor |
| `app_operator` | `*.view`, `container.logs`, `container.control`, `stack.deploy` |
| `viewer` | `*.view`, `container.logs`, `host.journal`, `host.apt.view`, `host.services.view` |
| `metrics_only` | `server.view`, `stack.view`, `container.view` |

- **Un permiso solo vale si el ámbito de la asignación es al menos de su nivel**: un `server_operator` asignado a un
  stack no obtiene `host.*` ni `resources.manage`. Así no hay combinación que escale del stack al host.
- **`stack.edit` no es de `app_operator`**: un compose corre como root y puede montar `/`, así que editarlo equivale a
  root en el servidor (ya avisado en el editor). «Desplegar» del operador de app = `stack.deploy` sin cambiar el
  compose. Igual con `container.terminal` (un contenedor privilegiado es root en el host): solo `server_operator`/
  `admin`.
- Herencia: organización → grupo → servidor → stack → contenedor. Un contenedor pertenece a su stack por
  `compose_project` del inventario; un contenedor sin proyecto solo hereda del servidor.
- API: `rbac.Resolve(bindings) Effective`; `Effective.Can(perm, Target{ServerID, GroupID, Stack, Container}) bool`;
  `Effective.Servers()`/`Stacks(serverID)` para filtrar listas. Pruebas de tabla exhaustivas (rol × ámbito × objetivo).

### D3 · Aplicación en el core
- Middleware `requirePerm(perm, targetFrom(r))` en **cada** ruta; `targetFrom` resuelve servidor (y su grupo),
  stack y contenedor desde la URL (el contenedor → stack con el inventario del hub).
- **Prueba guardiana**: recorre el router con `chi.Walk` y falla si una ruta de `/api/v1` no tiene permiso declarado
  (tabla `routePerms` única). Nadie puede añadir una ruta sin decidir su permiso.
- Respuesta sin permiso: `404` si el usuario no puede ver el objeto (no revela que existe), `403 forbidden` si lo ve
  pero no puede actuar. Toda denegación se audita (`result=denied`).
- Listas filtradas: `/servers` (solo servidores con `server.view` a nivel de servidor o superior), contenedores y
  stacks (solo los visibles), `/jobs/{id}` (solo su creador o `admin`), `/audit` y grabaciones (`audit.view`,
  `recordings.view`).
- Endpoint nuevo `GET /api/v1/me`: usuario, ámbitos y permisos efectivos resumidos para que el panel esconda lo que
  no puede usar, y `GET /api/v1/stacks` (stacks visibles de todos los servidores, para quien solo tiene stacks).
- `audit_event` gana `actor_id uuid NULL` y `scope jsonb` (tipo + ids); se sigue guardando `actor` (texto) para
  que el historial sobreviva a borrar usuarios.

### D4 · Altas de usuarios (sin correo)
- El admin crea una **invitación** (`user_invite`: hash del token, nombre de usuario, email opcional, asignación
  inicial, caduca en 72 h, un solo uso) y copia el enlace `…/invite#<token>`. El invitado pone contraseña y **activa
  TOTP antes de tener sesión `full`** (2FA obligatorio para todos, como ahora). Nada de SMTP en esta fase.

### D5 · Claves de API
- Formato `tlk_<id 12>_<secreto 32 base62>`; se guarda SHA-256 del secreto (entropía alta, no hace falta argon2),
  se muestra una sola vez. Campos: nombre, usuario dueño, asignaciones propias (mismo modelo `role_binding` con
  `subject_type=api_key`), caducidad obligatoria (por defecto 90 días, máximo 1 año), `last_used_at`, revocación.
- Permisos efectivos = **intersección** de las asignaciones de la clave y las del dueño en ese momento (si el dueño
  pierde permisos, la clave también).
- Una clave **nunca** puede: abrir terminales (ni el WebSocket), `users.manage`, `settings.manage`, gestionar
  claves, `host.reboot`. `Authorization: Bearer tlk_…`; sin cookie ni CSRF. Límite de peticiones por clave.

### D6 · OIDC (SSO)
- Proveedor genérico por descubrimiento (`github.com/coreos/go-oidc/v3` + `golang.org/x/oauth2`), flujo de código con
  PKCE, `state` y `nonce`. Configuración en Ajustes (emisor, client id, secreto cifrado con la clave de secretos,
  nombre del botón); desactivado por defecto.
- **Sin altas automáticas**: el login OIDC solo entra en un usuario existente cuyo `email` coincide con el claim
  `email` **con `email_verified=true`**; el `sub` se guarda en el primer login (`app_user.oidc_subject`) y a partir
  de ahí manda el `sub`.
- OIDC sustituye a la contraseña, **no al segundo factor**: después se pide TOTP o passkey como siempre.

### D7 · Grabaciones en el panel
- La grabación ya existe. Se añade la lista de sesiones (quién, dónde, cuándo, duración, motivo de cierre) y la
  descarga `.cast` para `recordings.view`. Reproducción en el panel: fuera de esta fase (*Ideas*).

### D8 · `talaiactl` v1
- Configuración `~/.config/talaia/config.toml` (0600) con `url` y `api_key`, o `TALAIA_URL`/`TALAIA_API_KEY`.
- Órdenes: `login`, `servers list`, `containers list <servidor>`, `containers <start|stop|restart> <servidor>
  <contenedor>`, `stacks list [servidor]`, `stacks <up|down|restart|pull> <servidor> <stack>`, `jobs wait <id>`,
  `version`. Salida en tabla o `--json`. Textos por i18n (go-i18n ya montado en `cli/i18n`). Confía en la CA del
  sistema o en `--ca-file`.

## Pasos

Cada paso sube el parche (`scripts/bump-version.sh`) con su entrada en los dos CHANGELOG; el cierre sube a 0.5.0.
Ejecutor por defecto: AgentRelay. Marca ✅ al terminar.

| # | Paso | Archivos | Terminado cuando | Ejecutor · esfuerzo |
|---|---|---|---|---|
| 1 | `rbac` puro (D2): permisos, roles, niveles, herencia, `Resolve`/`Can`/filtros | `core/internal/rbac/**` | pruebas de tabla rol × ámbito × objetivo, incluido «stack → nada de host» | AgentRelay · alto |
| 2 | Migración 0012 y store (D1, D4, D5): grupos, equipos, `role_binding`, campos de usuario, invitaciones, claves, `audit_event.actor_id/scope`, admin inicial | `core/internal/store/migrations/0012_rbac.sql`, `core/internal/store/{rbac,invites,apikeys}.go` | pruebas con Postgres; invariante del último admin en transacción | AgentRelay · alto |
| 3 | Aplicación (D3): `requirePerm`, `routePerms`, prueba guardiana `chi.Walk`, 404/403 y auditoría de denegaciones, listas filtradas, `/me`, `/stacks` | `core/internal/api/{authz.go,api.go,servers.go,containers.go,stacks.go,audit.go}` | **prueba del criterio**: usuario con un solo stack contra todas las rutas → solo su stack | AgentRelay · alto + revisión Opus |
| 4 | API de usuarios, equipos, grupos, asignaciones e invitaciones con auditoría | `core/internal/api/{users,teams,groups,invites}.go` | pruebas: último admin, desactivar revoca todo, invitación de un uso | AgentRelay · medio |
| 5 | Claves de API (D5) en el middleware de sesión y su API | `core/internal/api/{apikeys.go,session.go}` | pruebas: intersección con el dueño, prohibidos (terminal, usuarios, reinicio), caducidad, revocación | AgentRelay · alto |
| 6 | OIDC (D6) | `core/internal/oidc/**`, `core/internal/api/auth.go` | pruebas con un IdP falso (`httptest`): `email_verified=false` rechazado, `nonce`/`state` erróneos, sin usuario → rechazo | AgentRelay · alto |
| 7 | Panel: `/me` y permisos (esconder lo no permitido), vista «solo stacks» | `web/src/api/me.ts`, `web/src/auth/**`, `web/src/screens/**` (solo condiciones) | pruebas por rol; capturas de la vista de un operador de app | AgentRelay · medio |
| 8 | Panel: pantalla Usuarios y permisos (maqueta 5), equipos, grupos, invitación y su aceptación | `web/src/screens/settings/{Users,Teams,Invite}*.tsx`, locales | capturas claro/oscuro/móvil; pruebas | AgentRelay · medio |
| 9 | Panel: claves de API, ajustes OIDC, botón SSO en el login, grabaciones (D7) | `web/src/screens/settings/**`, `web/src/screens/Login*.tsx`, `core/internal/api/recordings.go` | capturas; pruebas | AgentRelay · medio |
| 10 | `talaiactl` v1 (D8) | `cli/**` | pruebas con `httptest`; i18n ca/es/en | AgentRelay · medio |
| 11 | Documentación EN+ES (permisos, usuarios, claves, OIDC, CLI), **revisión de seguridad (Opus)**, prueba real con un segundo usuario y cierre 0.5.0 | `docs/**`, ROADMAP, TODO | criterio cumplido con el mantenedor | Opus + mantenedor |

Paralelismo: 1 y 2 a la vez; 3 necesita 1 y 2; 4, 5 y 6 tras 3 (archivos distintos); 7-9 tras 3-6; 10 tras 5.

## Cómo seguir

`/clear` → `/model sonnet` → «ejecuta los pasos 1 y 2 de `.agents/plans/fase4/fase4.PLAN.md`» (orquestar con
AgentRelay y revisar). El paso 3 lo revisa Opus antes de aceptarlo. Ante un imprevisto de diseño, el ejecutor para
y lo anota aquí.
