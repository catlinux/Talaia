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

CLI en Go que usa la misma API REST con una clave de API. Pensada para scripts, pipelines de CI y
sincronización GitOps.

## 3. Conexión agente ↔ núcleo

### 3.1 Alta

1. El usuario pulsa **Añadir servidor** en el panel. El núcleo crea un token de un solo uso válido
   durante 15 minutos.
2. En el VPS: `curl -fsSL https://<núcleo>/install.sh | bash -s -- --token <token>` (o `apt install
   talaia-agent` + `talaia-agent enroll`).
3. El agente genera una clave privada que nunca sale del servidor y envía una CSR con el token.
4. La CA interna del núcleo firma un certificado de 90 días vinculado al ID del servidor. Se renueva
   automáticamente a los dos tercios de su vida.
5. El agente abre un stream gRPC persistente sobre mTLS hacia el puerto 443 del núcleo.
6. El agente envía su inventario inicial y el servidor aparece en la flota.

Implementado hasta ahora (fase 1, diseño en `.agents/plans/fase1/D-enrolamiento.DESIGN.md`, pendiente de
revisión):

- Hasta que el panel tenga usuarios (fase 3), el token se crea con `talaia-core token create --name
  <servidor>`, que también imprime la **huella de la CA** (SHA-256). `install.sh` recibe `--token` y
  `--ca-fingerprint`.
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

## 4. Modelo de datos (inicial)

| Entidad | Campos principales |
|---|---|
| `organization` | id, name |
| `user` | id, email, password_hash, totp_secret, locale |
| `passkey` | id, user_id, credential |
| `team` | id, name |
| `team_member` | team_id, user_id |
| `server_group` | id, name, tags |
| `server` | id, group_id, name, os, arch, agent_version, cert_fingerprint, last_seen, status |
| `stack` | id, server_id, name, source (git/local), compose, env_encrypted, current_revision |
| `stack_revision` | id, stack_id, compose, author_id, created_at |
| `container` | id, server_id, stack_id (opcional), docker_id, image, state — caché del inventario |
| `domain` | id, stack_id, host, port, tls_status |
| `role_binding` | subject (usuario/equipo), scope_type, scope_id, role |
| `job` | id, server_id, kind, payload, status, output, created_by |
| `alert_rule` | id, scope, metric, threshold, channels |
| `backup_plan` | id, target, schedule, destination, retention |
| `audit_event` | id, actor, action, scope, ip, result, created_at |
| `metric` | hypertable de TimescaleDB: server_id, container_id, ts, values |

## 5. Permisos

Los ámbitos forman un árbol: **organización → grupo → servidor → stack → contenedor**. Un rol concedido en
un nodo se aplica a todo lo que cuelga de él.

| Rol | Permisos |
|---|---|
| Administrador | Todo, incluidos usuarios y configuración |
| Operador de servidor | Todo en sus servidores: contenedores, servicios, paquetes, terminal |
| Operador de app | Ver, logs, reiniciar, desplegar en sus stacks; sin terminal ni acceso al anfitrión |
| Lector | Solo lectura |
| Solo métricas | Métricas y estado, sin logs |

Los permisos se comprueban siempre en el núcleo. El panel web solo oculta lo que el usuario no puede usar.

## 6. Resumen de seguridad

- Los agentes se conectan hacia fuera; los VPS no abren puertos para Talaia.
- mTLS con certificados por agente; los certificados se pueden revocar desde el panel.
- 2FA obligatorio; se admiten passkeys y OIDC.
- Secretos cifrados con `age`; nunca aparecen en los logs.
- Registro de auditoría de cada acción; sesiones de terminal grabadas.
- Paquetes e imágenes firmados (cosign); el agente verifica las firmas antes de autoactualizarse.
- Despliegue recomendado: panel accesible solo a través de WireGuard o Tailscale.

## 7. Ubicación de archivos en los servidores gestionados

| Ruta | Contenido |
|---|---|
| `/etc/talaia/agent.toml` | Configuración del agente |
| `/etc/talaia/certs/` | Clave y certificado del agente (modo 0600) |
| `/var/lib/talaia/` | Búfer local, estado |
| `/opt/talaia/stacks/<nombre>/` | `compose.yaml` y `.env` de cada stack |
