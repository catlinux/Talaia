# Arquitectura

[Read in English](../en/architecture.md) · [Volver al README](../../README.es.md)

> Estado: **diseño**. Este documento describe la arquitectura objetivo. Se actualiza a medida que se
> implementa cada fase.

## 1. Visión general

```
 Navegador (talaia-web) ─┐
 talaiactl ──────────────┼── HTTPS: REST + WebSocket ──▶ ┌────────────────────┐
 Webhooks de Git ────────┘                               │    talaia-core     │──▶ PostgreSQL 16
                                                         │                    │    + TimescaleDB
                                                         └─────────▲──────────┘
                                                                   │ stream gRPC bidireccional
                                                                   │ mTLS, TCP 443
                                                                   │ (abierto por el agente)
                                           ┌───────────────────────┼───────────────────────┐
                                     talaia-agent            talaia-agent             talaia-agent
                                     (vps-web-01)            (vps-db-01)              (rpi-casa)
                                     Docker · systemd · journald · apt · métricas · PTY · Caddy · restic
```

## 2. Componentes

### 2.1 talaia-core

Binario único en Go. Se despliega con un `compose.yaml` (núcleo + PostgreSQL) en un VPS pequeño o en un
servidor doméstico.

| Módulo | Responsabilidad |
|---|---|
| `api` | API REST + WebSocket para el panel web y la CLI |
| `auth` | Sesiones, TOTP, passkeys WebAuthn, OIDC, claves de API |
| `rbac` | Roles y ámbitos (organización → grupo → servidor → stack → contenedor) |
| `hub` | Servidor gRPC; mantiene un stream por agente conectado; enruta tareas y eventos |
| `jobs` | Cola de tareas persistente (River sobre PostgreSQL), reintentos, programación |
| `alerts` | Evalúa reglas sobre métricas y eventos, envía notificaciones |
| `deploy` | Orígenes Git, builds, pipeline de despliegue, reversiones |
| `secrets` | Cifra las variables secretas con `age`; solo las descifra el agente destino |
| `ca` | Autoridad de certificación interna para los certificados de los agentes |
| `audit` | Registro inmutable de cada acción: quién, qué, dónde, desde qué IP, resultado |
| `store` | Acceso a base de datos (sqlc) y migraciones |

El núcleo también sirve el panel web compilado como archivos estáticos.

### 2.2 talaia-agent

Binario estático en Go (~15 MB), distribuido como paquete `.deb` con una unidad systemd.
Objetivos: < 30 MB de RAM y < 1 % de CPU en reposo.

| Módulo | Usa | Hace |
|---|---|---|
| `docker` | API de Docker Engine (socket) | Contenedores, imágenes, volúmenes, redes, estadísticas, exec |
| `compose` | `docker compose` v2 | Stacks en `/opt/talaia/stacks/<nombre>/` |
| `systemd` | D-Bus (go-systemd) | Listar, arrancar, parar, habilitar unidades |
| `journal` | sdjournal | Leer y seguir logs con filtros |
| `apt` | apt / dpkg | Actualizaciones pendientes y de seguridad, actualizar, reinicio necesario |
| `metrics` | /proc, gopsutil | CPU, RAM, disco, red, estadísticas por contenedor |
| `pty` | creack/pty | Terminales interactivas (anfitrión o contenedor) |
| `files` | sistema de archivos | Explorar, leer, editar (dentro de rutas permitidas) |
| `firewall` | nftables / ufw | Leer y cambiar reglas |
| `backup` | restic | Ejecutar planes de copia y restauraciones |
| `proxy` | API de administración de Caddy | Dominios, TLS, rutas |

Cada módulo se puede desactivar en `/etc/talaia/agent.toml`. El agente guarda 24 h de métricas y eventos
en un búfer local y los envía al reconectar.

### 2.3 talaia-web

Aplicación de una sola página en React + TypeScript (Vite). TanStack Query para los datos, WebSocket para
las actualizaciones en directo, xterm.js para las terminales, Monaco para los editores, uPlot para los
gráficos e i18next para las traducciones (ca/es/en).

### 2.4 talaiactl

Cliente de línea de comandos en Go para scripts, pipelines de CI y sincronización GitOps. Se autentica
con una clave de API (`Authorization: Bearer tlk_…`) y ofrece `login`, `servers list`,
`containers list|start|stop|restart`, `stacks list|up|down|restart|pull`, `jobs wait`, `version` y
`help`. Lee `url` y `api_key` de `~/.config/talaia/config.toml` (el directorio que devuelve
`os.UserConfigDir()`; `%AppData%\talaia\config.toml` en Windows), creado con modo 0600 y rechazado en Unix si el grupo
u otros usuarios pueden leerlo. `TALAIA_URL`, `TALAIA_API_KEY`, `TALAIA_CA_FILE` y los parámetros
equivalentes sustituyen al archivo. La salida es una tabla alineada por defecto o `--json` con los
datos crudos; las acciones admiten `--wait`, y `jobs wait` consulta cada 2 segundos hasta `--timeout`
(10 minutos por defecto). TLS usa las raíces del sistema más un `--ca-file` opcional; la verificación
no se puede desactivar.

## 3. Conexión agente ↔ núcleo

### 3.1 Alta

1. El usuario pulsa **Añadir servidor** en el panel. El núcleo crea un token de un solo uso válido
   durante 15 minutos.
2. En el VPS: la orden que muestra el panel (descarga `install.sh` del núcleo y lo comprueba) o, como alternativa,
   `apt install talaia-agent` + `talaia-agent enroll`.
3. El agente genera una clave privada que nunca sale del servidor y envía una CSR con el token.
4. La CA interna del núcleo firma un certificado de 90 días vinculado al ID del servidor. Se renueva
   automáticamente a los dos tercios de su vida.
5. El agente abre un stream gRPC persistente sobre mTLS hacia el puerto 443 del núcleo.
6. El agente envía su inventario inicial y el servidor aparece en la flota.

Implementado hasta ahora (fase 1, diseño en `.agents/plans/fase1/D-enrolamiento.DESIGN.md`, pendiente de
revisión):

- El token se crea desde el panel (**Añadir servidor**, `POST /api/v1/servers/enrollment-tokens`, solo con
  sesión completa y auditado) o desde la consola con `talaia-core token create --name <servidor>`. Ambos dan
  la **huella de la CA** (SHA-256) y una orden lista para pegar; `install.sh` recibe `--url`, `--token` y
  `--ca-fingerprint`.
- El núcleo sirve `install.sh` y los paquetes del agente (`/install.sh`, `/agent/talaia-agent_<arq>.deb`,
  construidos dentro de su imagen). La orden descarga el script antes de que el servidor confíe en la CA, así
  que lleva el SHA-256 del script y se niega a ejecutar uno distinto; el paquete se descarga después por una
  conexión que solo confía en la CA, tras comprobar la huella.
- Ancla de confianza: el agente descarga `GET /ca.pem` una vez sin verificar y continúa **solo si** su
  SHA-256 coincide con la huella; todo lo demás va con TLS 1.3 verificado.
- El token se guarda con hash, caduca a los 15 minutos, se consume de forma atómica y un token rechazado
  nunca dice por qué (desconocido, usado o caducado). `POST /enroll` se limita a 10 peticiones por minuto
  y por IP.
- La CA ignora el subject de la CSR, exige una clave ECDSA P-256 y fija el common name al ID del servidor.
  El núcleo toma la identidad del agente de ese certificado; `talaia-core server revoke <id>` lo bloquea en
  la siguiente conexión o renovación.
- `insecure_dev` (núcleo) y `TALAIA_AGENT_INSECURE_DEV` (agente) pasan a un enlace sin cifrar solo para
  desarrollo local; `make dev` los usa.

### 3.2 Protocolo

Contrato en `proto/agent.proto` (gestionado con `buf`). Resumen:

```proto
service AgentHub {
  rpc Connect(stream AgentMsg) returns (stream CoreMsg);
}

message CoreMsg {               // núcleo → agente
  string job_id = 1;            // clave de idempotencia
  oneof cmd {
    ComposeUp   compose_up = 10;
    ExecPty     exec_pty   = 11;
    AptUpgrade  apt        = 12;
    ReadLogs    logs       = 13;
    // un mensaje por cada orden tipada
  }
}

message AgentMsg {              // agente → núcleo
  oneof msg {
    Heartbeat heartbeat = 1;    // cada 10 s
    Metrics   metrics   = 2;    // cada 15 s
    JobEvent  job_event = 3;    // progreso, salida, resultado
    Inventory inventory = 4;    // al conectar y cuando cambia
  }
}
```

Principios:

- **Solo órdenes tipadas.** Nada de shell arbitrario, salvo las terminales interactivas que el usuario
  abre explícitamente y que quedan grabadas.
- **Tareas idempotentes.** Cada orden tiene un `job_id`; repetirla no repite el efecto.
- **Reconexión** con espera exponencial y aleatoriedad (1 s → 60 s).
- **Contrato versionado.** Los campos se añaden, nunca se renumeran. Núcleo y agente negocian una versión
  de protocolo al conectar.

### 3.3 Terminales interactivas

Una terminal de contenedor y una del anfitrión usan el mismo flujo. El panel pide una sesión al núcleo
(`POST /api/v1/servers/{serverID}/containers/{container}/terminal` o
`POST /api/v1/servers/{serverID}/terminal`), recibe un ticket de un solo uso y abre
`GET /api/v1/terminal/{sessionID}/ws`. El núcleo guarda la sesión en el gestor de terminales y envía al
agente un mensaje tipado `TerminalOpen` con `TerminalTarget` puesto a contenedor o anfitrión. El agente
arranca un PTY; la salida y los cambios de tamaño vuelven y se graban, mientras que las pulsaciones no se
graban. El panel envía primero el ticket y después datos binarios y mensajes de control de tamaño.

Una terminal del anfitrión necesita además `confirm` y un código TOTP nuevo con la misma protección
contra reutilización que el acceso y el cambio de contraseña, y el agente debe informar de que la admite.
Arranca `/bin/bash -l` o `/bin/sh -l` como root con un entorno limpio, está limitada a 2 sesiones por
agente y una por usuario y servidor, comparte el límite de 4 sesiones por servidor y se audita como
`host_terminal_open` y `host_terminal_close`. Se puede desactivar con `[terminal] host = false` en
`/etc/talaia/agent.toml` o con `TALAIA_AGENT_TERMINAL_HOST=false`.

## 4. Modelo de datos (implementado)

Este es el esquema creado por las migraciones hasta `0013_oidc.sql`. Los stacks y contenedores aún no
son tablas: los stacks viven en disco y los contenedores proceden del inventario del agente. Las tablas
de dominios, alertas, copias de seguridad y métricas pertenecen a fases posteriores de la hoja de ruta.

| Entidad | Campos principales |
|---|---|
| `organization` | id, name |
| `app_user` | id, username, display_name, email, password_hash, campos TOTP, intentos fallidos, locked_until, disabled_at, oidc_subject |
| `recovery_code` | id, user_id, code_hash, used_at |
| `session` | id, token_hash, user_id, etapa (`mfa_pending` o `full`), caducidad, revoked_at, IP, user agent, fallos MFA |
| `trusted_device` | id, user_id, token_hash, pin_hash, nombre, marcador del navegador de 180 días, contadores y fechas |
| `passkey` | id, user_id, credential_id, public_key, contador de firmas, transportes, indicadores de copia |
| `webauthn_ceremony` | id, user_id, kind, session_data, expires_at |
| `server` | id, name, group_id, serial y caducidad del certificado, created_at, last_seen, revoked_at, removed_at |
| `enrollment_token` | id, token_hash, server_name, expires_at, used_at |
| `server_group` | id, name, created_at |
| `team` | id, name, created_at |
| `team_member` | team_id, user_id, created_at |
| `role_binding` | id, subject_type (`user`, `team`, `api_key`), subject_id, scope_type (`organization`, `group`, `server`, `stack`, `container`), scope_server_id, scope_group_id, scope_name, role, created_by, created_at |
| `user_invite` | id, token_hash, username, email, role, campos de ámbito, created_by, created_at, expires_at, used_at |
| `api_key` | id, key_id, name, owner_id, secret_hash, created_at, expires_at, last_used_at, revoked_at |
| `oidc_config` | una fila: enabled, issuer, client_id, client_secret_enc, button_label, updated_at, updated_by |
| `audit_event` | id, at, actor, actor_id, action, IP, result, details, scope |
| `job` | id, server_id, kind, params, state, message, output, created_by, fechas |
| `terminal_session` | id, server_id, contenedor o `@host`, shell, created_by, privileged, fechas, close_reason, recorded_bytes |
| `terminal_chunk` | session_id, seq, datos cifrados |

La migración `0012_rbac.sql` da además a cada usuario de una instalación existente la asignación de
administrador de la organización, añade `group_id` a `server`, `display_name`/`email`/`disabled_at`/
`oidc_subject` a `app_user`, y `actor_id`/`scope` a `audit_event`. La migración `0013_oidc.sql` añade
`oidc_config` con una sola fila (`id boolean PRIMARY KEY DEFAULT true CHECK (id)`).

## 5. Permisos

Hay una sola organización. Los **grupos de servidores** agrupan servidores, los **equipos** agrupan
usuarios y una **asignación de rol** da un rol fijo a un usuario, un equipo o una clave de API sobre un
ámbito.

```text
Organización → Grupo → Servidor → Stack → Contenedor
```

Un ámbito de stack o contenedor se identifica por su servidor y su nombre. Un contenedor que pertenece
a un proyecto Compose hereda las asignaciones del stack; un contenedor suelto hereda solo del servidor
y de los niveles superiores. Una asignación se aplica a su nodo y a todo lo que cuelga de él.

### 5.1 Roles

| Rol | Permisos antes de filtrar por ámbito |
|---|---|
| `admin` | Todos los permisos |
| `server_operator` | Todos los permisos de contenedor, stack y servidor (incluidas las terminales de contenedor y del anfitrión), pero no los de organización |
| `app_operator` | Todos los permisos `.view`, más `container.logs`, `container.control` y `stack.deploy` |
| `viewer` | Todos los permisos `.view`, más `container.logs` |
| `metrics_only` | `server.view`, `stack.view`, `container.view` |

La lista del rol se filtra además por el ámbito mínimo de cada permiso. `audit.view` y
`recordings.view` son solo de organización; `host.*` y `server.agent_update` necesitan al menos un
servidor; `stack.edit` necesita al menos un stack; `container.*` se puede conceder en un contenedor.

| Ámbito mínimo | Permisos |
|---|---|
| Contenedor | `container.view`, `container.logs`, `container.control`, `container.terminal` |
| Stack | `stack.view`, `stack.deploy`, `stack.edit`, `container.remove` |
| Servidor | `server.view`, `resources.manage`, `host.services.view`, `host.services.control`, `host.journal`, `host.apt.view`, `host.apt.upgrade`, `host.reboot`, `host.terminal`, `server.agent_update` |
| Organización | `server.remove`, `server.enroll`, `users.manage`, `settings.manage`, `audit.view`, `recordings.view`, `apikeys.manage` |

Un permiso cuenta solo cuando el ámbito de la asignación es al menos tan amplio como su ámbito mínimo.
Un `server_operator` asignado a un stack obtiene allí permisos de contenedor y stack, pero nunca acceso
al host. Un grupo no tiene un conjunto de permisos propio; una asignación allí se aplica a sus servidores y a sus
descendientes.

`app_operator` carece a propósito de `stack.edit`, `container.terminal` y `host.terminal`: un archivo
Compose se ejecuta como root y un shell privilegiado llega al host. Puede desplegar archivos que ya
existen; cambiarlos o abrir una terminal no. Un `server_operator` asignado a un stack no recibe endpoints
del host, pero `stack.edit` sigue equivaliendo a root a través de Compose y debe tratarse como tal.

### 5.2 Aplicación

Cada ruta protegida declara su permiso en la única tabla `routePerms` de
`core/internal/api/authz.go`. La prueba guardiana con `chi.Walk` falla si una ruta no tiene
declaración, así que un endpoint nuevo no puede quedar público por descuido. El middleware resuelve las
asignaciones efectivas y el objetivo (organización, grupo, servidor, stack o contenedor) y aplica la
regla.

- Quien no puede ver el objeto recibe **404 Not Found**; quien lo ve pero no tiene la operación recibe
  **403 Forbidden**.
- Cada denegación se escribe en la auditoría como `authz_denied`, con actor, permiso, método, ruta,
  resultado y ámbito (y el id de la clave de API cuando corresponde).
- Las listas filtran servidores, stacks y contenedores por visibilidad. Una tarea solo la ve su creador
  o quien tenga `users.manage`.
- El panel usa `GET /api/v1/me` y solo esconde controles; nunca sustituye la comprobación del núcleo.

### 5.3 Claves de API

Una clave de API tiene sus propias asignaciones de rol (`subject_type = api_key`). Sus permisos
efectivos son la **intersección** de esas asignaciones y las actuales de su dueño, así que pierde acceso
cuando lo pierde el dueño. La lista negra de claves siempre se aplica: no pueden abrir terminales de
contenedor o del anfitrión, gestionar usuarios, equipos, grupos, claves ni ajustes, reiniciar un
servidor ni llegar a los endpoints de inicio de sesión. Las rutas marcadas como `noAPIKey` se rechazan
aunque las asignaciones de la clave las permitieran.

Las claves se autentican con `Authorization: Bearer tlk_…` y no necesitan cookie, token CSRF ni
cabecera `Origin`. Están limitadas a 600 peticiones por minuto por clave, con un límite adicional por
IP; el secreto se guarda con hash y se muestra una vez.

### 5.4 Último administrador

Al menos un usuario activo debe conservar el rol de administrador de la organización, directo o a través
de un equipo. Desactivar al último administrador, o quitar o degradar la última asignación de
administrador de organización (directa o mediante un equipo), devuelve `409 last_admin`; un usuario no
puede desactivar su propia cuenta. La comprobación se ejecuta con un bloqueo consultivo dentro de la
transacción.

La guía completa para la persona usuaria es [permisos.md](permisos.md).

## 6. Resumen de seguridad

- Los agentes se conectan hacia fuera; los VPS no abren puertos para Talaia. mTLS con certificados por
  agente, revocables desde el panel.
- Los permisos se aplican en el núcleo: cinco roles fijos sobre organización → grupo → servidor → stack
  → contenedor. Una denegación es 404 cuando el objeto está oculto y 403 cuando se ve pero la operación
  no está permitida; cada denegación se audita. El panel solo esconde controles. Véase [permisos.md](permisos.md).
- 2FA obligatorio, claves de acceso y OIDC solo para usuarios existentes: el proveedor sustituye la
  contraseña, no el segundo factor. OIDC exige correo verificado, vincula el subject en el primer acceso
  y no crea cuentas.
- Las claves de API tienen ámbito, se guardan con hash, se muestran una vez, se limitan a 600
  peticiones/minuto y a la intersección de sus asignaciones con las actuales del dueño. No pueden usar
  terminales, administración de usuarios/ajustes, gestión de claves, reinicio ni rutas de acceso.
- Las invitaciones son de un solo uso, caducan a las 72 horas, se guardan con hash y siguen exigiendo
  configurar TOTP.
- Los secretos (TOTP, secreto de cliente OIDC, trozos de terminal) se cifran con la clave de secretos;
  nunca aparecen en los logs.
- Registro de auditoría de cada acción y denegación; sesiones de terminal grabadas. Los administradores y
  operadores de servidor equivalen a root en los servidores que gestionan: véase [seguridad.md](seguridad.md).
- Paquetes e imágenes firmados (cosign); el agente verifica las firmas antes de autoactualizarse.
- Despliegue recomendado: panel accesible solo a través de WireGuard o Tailscale.

### 6.1 Autenticación y autorización del panel (implementadas)

El primer administrador se crea desde la consola del servidor (`talaia-core user create --username
<nombre>`); nunca hay una página pública de alta. Los demás usuarios llegan por invitación y deben
configurar TOTP.

- Contraseña (12–256 caracteres) con argon2id; TOTP obligatorio desde el primer acceso, un código no se
  puede usar dos veces y el secreto TOTP se cifra con AES-256-GCM (`data_dir/secrets.key`).
- 10 códigos de recuperación de un solo uso, que se muestran una vez. Recuperación por consola:
  `talaia-core user reset-2fa` y `talaia-core session revoke-all`.
- Cookie de sesión `__Host-talaia_session` (`Secure`, `HttpOnly`, `SameSite=Strict`), guardada con hash;
  12 h sin actividad y 7 días como máximo; token nuevo tras el segundo factor.
- CSRF: cuerpo JSON y un `Origin` permitido en toda petición que modifica. Cabeceras de seguridad (CSP,
  HSTS, `nosniff`, `no-referrer`).
- OIDC usa descubrimiento, código de autorización con PKCE, `state` y `nonce` en una cookie sellada de
  10 minutos, y verifica el ID token. Los fallos redirigen al error genérico del acceso y se auditan;
  nunca revelan si existe una cuenta.
- Las claves de API usan `Authorization: Bearer tlk_…` sin cookie, token CSRF ni `Origin`; se rechazan
  en las rutas marcadas `noAPIKey` aunque sus asignaciones de rol las permitieran.
- Fuerza bruta: 10 intentos por IP cada 15 minutos, 5 fallos bloquean la cuenta 15 minutos; los errores
  nunca dicen qué falló. Cada acceso, fallo, denegación y cambio queda en el registro de auditoría, sin
  secretos.
- Debe quedar al menos un administrador de organización activo; el almacén se niega a desactivar al
  último administrador o a quitar o degradar la última asignación de administrador de organización
  (`409 last_admin`).

## 7. Ubicación de archivos en los servidores gestionados

| Ruta | Contenido |
|---|---|
| `/etc/talaia/agent.toml` | Configuración del agente |
| `/etc/talaia/certs/` | Clave y certificado del agente (modo 0600) |
| `/var/lib/talaia/` | Búfer local, estado |
| `/opt/talaia/stacks/<nombre>/` | `compose.yaml` y `.env` de cada stack |

## 8. Operaciones sobre el servidor entero (fase 3)

La fase 3 está implementada y se cerró en 0.4.0 tras la prueba real en dos servidores y su revisión de
seguridad. Cubre la pantalla del servidor, el estado y las actualizaciones de apt, los servicios
systemd, la lectura del journal, la terminal del anfitrión y la pantalla de auditoría. La guía de uso es
[servidor.md](servidor.md).

### 8.1 Órdenes tipadas y tipos de tarea

El núcleo nunca envía una línea de shell. Cada operación es un `CoreMsg` tipado con un `job_id`; el
agente vuelve a comprobar el mensaje y responde con mensajes `JobEvent`.

| Tipo de tarea | Orden | Notas |
|---|---|---|
| `apt_status` | `AptStatus{refresh:false}` | Lee el resumen apt en caché. |
| `apt_refresh` | `AptStatus{refresh:true}` | Ejecuta `apt-get update` antes; caducidad mayor. |
| `apt_upgrade` | `AptUpgrade{mode}` | `security` o `all`; el agente calcula la lista de paquetes. |
| `host_reboot` | `HostReboot{hostname}` | Programa `systemctl reboot` cinco segundos después. |
| `service_action` | `ServiceAction{unit,action}` | Iniciar, parar, reiniciar, recargar, habilitar o deshabilitar. |
| `journal_read` | `ReadJournal{...}` | Entradas del journal y cursor en la salida de la tarea. |

La terminal no es una tarea: el gestor mantiene la sesión viva y el navegador se conecta por WebSocket.

Las incorporaciones al protocolo son las órdenes `ServiceAction`, `ReadJournal`, `AptStatus`, `AptUpgrade` y
`HostReboot`, `AptSummary` y `host_terminal` en `Inventory`, `unit_file_state` en `SystemdUnit`, y
`TerminalOpen.target` con `TerminalTarget` (`UNSPECIFIED` sigue siendo el contenedor por compatibilidad).

### 8.2 Endpoints

Todas las rutas están bajo `/api/v1` y necesitan una sesión completa. Las rutas de contenedores y
stacks de la fase 2 no cambian.

| Método y ruta | Cuerpo o consulta | Tarea | Acción de auditoría |
|---|---|---|---|
| `GET /servers/{serverID}/services` | - | - | ninguna (lectura) |
| `POST /servers/{serverID}/services/{unit}/actions` | `{action,confirm}` | `service_action` | `service_<action>` |
| `POST /servers/{serverID}/apt/status` | `{refresh}` (opcional) | `apt_status` o `apt_refresh` | `apt_status` |
| `POST /servers/{serverID}/apt/upgrade` | `{mode:"security" or "all",confirm:true}` | `apt_upgrade` | `apt_upgrade` |
| `POST /servers/{serverID}/reboot` | `{confirm:"<nombre de máquina>"}` | `host_reboot` | `host_reboot` |
| `POST /servers/{serverID}/journal` | filtros (abajo) | `journal_read` | `journal_read` |
| `POST /servers/{serverID}/terminal` | `{confirm:true,code,shell,cols,rows}` | sesión de terminal | `host_terminal_open` / `host_terminal_close` |
| `GET /terminal/{sessionID}/ws` | WebSocket, ticket primero | - | - |
| `GET /audit` | `limit,before,action,server_id,actor` | - | ninguna (lectura) |
| `GET /jobs/{jobID}` | - | - | - |

Cuerpo del journal: `unit`, `max_priority` (0-7), `since` y `until` (RFC 3339), `search`
(literal, como máximo 200 caracteres), `boot` (`all`, `current`, `previous`), `tail` (1-1000,
200 por defecto) y `after_cursor` para seguir. La entrada de auditoría guarda la unidad y el arranque,
no la búsqueda ni las líneas.

Los errores usan los mismos códigos que el resto de la API: `invalid_request`,
`confirmation_required`, `agent_offline`, `operation_in_progress`, `self_management`,
`too_many_terminals`, `terminal_exists`, `code_invalid`, `host_terminal_disabled`,
`agent_busy` y `terminal_open_failed`.

### 8.3 apt en una unidad transitoria de systemd

El agente calcula la lista de paquetes de una actualización de seguridad; el núcleo nunca envía nombres.
La actualización corre con `systemd-run` en una unidad llamada `talaia-apt-<id corto>`:

```text
systemd-run --unit=talaia-apt-<id> --collect --property=Type=exec
  --property=RemainAfterExit=yes
  --property=StandardOutput=append:/var/lib/talaia/apt/<job>.log
  --setenv=DEBIAN_FRONTEND=noninteractive --setenv=LC_ALL=C
  -- apt-get -y -o Dpkg::Options::=--force-confdef
     -o Dpkg::Options::=--force-confold -o DPkg::Lock::Timeout=120
     --no-remove upgrade --with-new-pkgs
```

`security` usa `install --only-upgrade -- <paquetes>`. El agente escribe
`/var/lib/talaia/apt/<job>.json` y consulta `systemctl show`; emite `RUNNING` con la cola del log
cada pocos segundos y retoma la unidad tras un reinicio del agente. Solo hay una actualización apt o un
reinicio a la vez. Los ficheros de estado se limpian a las 24 horas y los logs a los 30 días.

### 8.4 Flujo de servicios y journal

Para una acción de servicio el núcleo comprueba que la unidad estaba en el último inventario, que
`confirm` está presente cuando la acción lo necesita y que no se está parando, reiniciando o
deshabilitando `talaia-agent.service`. Crea una tarea `service_action` y envía la `ServiceAction`
tipada; el agente ejecuta `systemctl --no-ask-password` con un verbo fijo, devuelve el estado nuevo y
refresca el inventario.

Para una lectura del journal el núcleo valida todos los filtros, crea una tarea `journal_read` y envía
`ReadJournal`. El agente ejecuta `journalctl --output=json --no-pager` con argumentos sueltos, recorta
cada mensaje a 4 KiB y el total a 256 KiB, y devuelve `{entries,cursor}`. El panel sigue el cursor cada
3 segundos.

### 8.5 Flujo de la terminal del anfitrión

1. El panel pide `POST /servers/{serverID}/terminal` con `confirm`, un `code` TOTP nuevo, el shell y
   el tamaño.
2. El núcleo comprueba la sesión completa, la conexión del agente y la capacidad `host_terminal`, aplica
   el limitador compartido, verifica el código TOTP con su protección contra reutilización y pide una
   sesión al gestor de terminales.
3. El gestor reserva los límites, guarda la sesión (contenedor `@host`), crea el grabador y envía la
   `TerminalOpen` tipada con `TerminalTarget.HOST`.
4. El agente arranca un shell de inicio de sesión en un PTY local; el gestor audita
   `host_terminal_open`, emite un ticket de 30 segundos y responde 201.
5. El navegador abre el WebSocket, envía el ticket y la sesión queda viva. La salida y los cambios de
   tamaño se graban cifrados; las pulsaciones no se graban. Al cerrar se audita
   `host_terminal_close`.

### 8.6 Datos en el servidor gestionado

| Ruta | Contenido |
|---|---|
| `/var/lib/talaia/apt/<job>.json` | Estado de la tarea apt para retomarla tras un reinicio del agente. |
| `/var/lib/talaia/apt/<job>.log` | Salida de apt, conservada 30 días. |
| `talaia-apt-<id>` | Unidad transitoria de systemd de una actualización apt. |
| `talaia-reboot` | Unidad transitoria que ejecuta `systemctl reboot`. |
