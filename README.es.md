<h1 align="center">Talaia</h1>

<p align="center">
  <b>Panel de control autoalojado para flotas de VPS Debian.</b><br>
  Gestiona un único contenedor Docker o un servidor entero, desde un solo lugar.
</p>

<p align="center">
  <a href="README.md">Read in English</a> ·
  <a href="ROADMAP.es.md">Hoja de ruta</a> ·
  <a href="TODO.es.md">TODO</a> ·
  <a href="CHANGELOG.es.md">Registro de cambios</a> ·
  <a href="LICENSE">Licencia</a>
</p>

<p align="center">
  <img alt="Versión" src="https://img.shields.io/badge/versi%C3%B3n-0.0.1-0B7285">
  <img alt="Estado" src="https://img.shields.io/badge/estado-fase%20de%20dise%C3%B1o-B86E00">
  <img alt="Licencia" src="https://img.shields.io/badge/licencia-WNCL--1.0-56666F">
  <img alt="Idiomas" src="https://img.shields.io/badge/i18n-ca%20%C2%B7%20es%20%C2%B7%20en-0B7285">
</p>

> [!WARNING]
> Talaia está en **fase de diseño**. Todavía no hay código utilizable. Este repositorio contiene por ahora
> la definición del proyecto, la hoja de ruta y las maquetas. Consulta la [hoja de ruta](ROADMAP.es.md)
> para ver qué viene a continuación.

---

## Índice

- [¿Qué es Talaia?](#qué-es-talaia)
- [¿Por qué otro panel?](#por-qué-otro-panel)
- [Dos niveles de gestión](#dos-niveles-de-gestión)
- [Funcionalidades previstas](#funcionalidades-previstas)
- [Arquitectura](#arquitectura)
- [Sistemas soportados](#sistemas-soportados)
- [Instalación](#instalación)
- [Tecnologías](#tecnologías)
- [Idiomas (i18n)](#idiomas-i18n)
- [Estructura del repositorio](#estructura-del-repositorio)
- [Maquetas](#maquetas)
- [Versiones](#versiones)
- [Flujo de trabajo](#flujo-de-trabajo)
- [Contribuir](#contribuir)
- [Seguridad](#seguridad)
- [Créditos y licencia](#créditos-y-licencia)

---

## ¿Qué es Talaia?

*Talaia* es la palabra catalana para la torre de vigilancia de la costa: desde un solo punto se ve todo el
litoral y se da la alarma si algo va mal. Es exactamente lo que hace este panel con tus servidores.

Talaia es un **panel web autoalojado** para gestionar una flota de VPS basados en Debian. Se compone de:

- **`talaia-core`**: un servidor central (API, base de datos, cola de tareas, alertas) que alojas tú.
- **`talaia-agent`**: un binario pequeño instalado en cada VPS. Se conecta *hacia fuera* con el núcleo,
  así que tus servidores no necesitan ningún puerto abierto para Talaia.
- **`talaia-web`**: el panel web, disponible en **catalán, castellano e inglés**.
- **`talaiactl`**: una herramienta de línea de comandos que hace todo lo que hace el panel.

## ¿Por qué otro panel?

Cada herramienta existente resuelve una parte del problema. Talaia toma la mejor idea de cada una:

| Herramienta | Qué toma Talaia | Qué mejora Talaia |
|---|---|---|
| Portainer | Gestión visual de contenedores, equipos y permisos, plantillas de apps | Permisos finos y multiservidor incluidos, no son extras de pago |
| Komodo | Un agente por servidor, stacks desde Git, sincronización declarativa (GitOps) | Interfaz más cómoda para el día a día y gestión completa del sistema |
| Coolify / Dokploy | Despliegue desde Git, dominios con SSL automático, copias de bases de datos | El resultado son stacks Compose normales, sin formatos propios |
| Dockge | El `compose.yaml` en disco es la fuente de verdad | Mismo principio, con historial de revisiones |
| Cockpit / Webmin | Gestión del servidor entero: systemd, journald, paquetes, cortafuegos, terminal | Pensado para muchos servidores a la vez |
| Beszel / Netdata | Métricas ligeras por servidor y por contenedor | Integradas con las alertas y la vista de flota |
| Uptime Kuma | Comprobaciones HTTP/TCP y notificaciones | Vinculadas automáticamente a cada app y dominio |
| Ansible / Semaphore | Ejecutar la misma tarea en muchos servidores | Acciones predefinidas, sin escribir YAML |
| Teleport | Terminal web auditada con grabación de sesiones | Integrada, sin infraestructura adicional |

## Dos niveles de gestión

Es el requisito principal del proyecto. Talaia trabaja a dos niveles sobre el mismo modelo de datos:

**Modo contenedor: una app, aislada**

- Stacks Compose y contenedores sueltos
- Editar `compose.yaml` y variables de entorno, con historial de revisiones
- Logs, estadísticas y terminal dentro del contenedor
- Desplegar, reiniciar y volver a una revisión anterior
- Dominio y SSL, copias de volúmenes, webhooks de despliegue
- Adoptar contenedores y proyectos Compose creados fuera de Talaia

**Modo servidor: la máquina entera**

- Métricas del sistema, discos y red
- Servicios systemd y logs de journald
- Paquetes apt, actualizaciones de seguridad y avisos de «reinicio necesario»
- Cortafuegos (nftables/ufw) y fail2ban
- Usuarios, claves SSH, cron y timers
- Explorador de archivos, terminal del anfitrión y auditoría de bastionado

Los permisos siguen la misma jerarquía y se heredan hacia abajo:

```
Organización → Grupo (prod, clientes…) → Servidor → Stack / App → Contenedor
```

Puedes dar acceso a una persona a **una sola app** sin que llegue a ver el servidor donde se ejecuta.

**Sin dependencia.** Los stacks se guardan en disco en `/opt/talaia/stacks/<nombre>/compose.yaml`. Si
desinstalas Talaia, todo sigue funcionando y puedes gestionarlo con `docker compose` como siempre.

## Funcionalidades previstas

Cada funcionalidad indica la versión donde está prevista. Detalle en [ROADMAP.es.md](ROADMAP.es.md).

| Área | MVP (0.4.0) | v1 (1.0.0) | Más adelante |
|---|---|---|---|
| Contenedores y stacks | Acciones, logs en directo, editor Compose, terminal del contenedor, imágenes/volúmenes/redes, adoptar existentes | Historial de revisiones y reversión, avisos de imágenes nuevas | Podman |
| Servidor entero | Métricas, systemd, journald, actualizaciones apt, terminal del anfitrión | Cortafuegos, fail2ban, usuarios y claves SSH, cron, explorador de archivos, auditoría de bastionado | — |
| Despliegues y apps | — | Stacks desde Git, builds con Dockerfile/Nixpacks, despliegue sin cortes, dominios + SSL (Caddy), catálogo de plantillas | Entornos de prueba por pull request |
| Monitorización y alertas | — | Historial de métricas, reglas de alerta, comprobaciones HTTP/TCP, caducidad de certificados, Telegram/ntfy/correo/Discord/webhook | Página de estado pública |
| Copias de seguridad | — | Planes con restic, volcados coherentes de bases de datos, S3/B2/SFTP, restaurar desde el panel | Prueba de restauración mensual automática |
| Automatización de flota | — | Acciones en bloque, procedimientos programados, sincronización GitOps | Playbooks de Ansible, creación de VPS vía API del proveedor |
| Usuarios y seguridad | Usuario único con 2FA, registro de auditoría | Usuarios, equipos, roles por ámbito, passkeys, SSO OIDC, grabación de sesiones de terminal, claves de API con permisos | — |

## Arquitectura

```
 Navegador / talaiactl / webhooks de Git
              │  HTTPS (REST + WebSocket)
              ▼
 ┌───────────────────────────────┐
 │          talaia-core          │   PostgreSQL 16 + TimescaleDB
 │ API · auth · RBAC · auditoría │
 │ hub de agentes · tareas       │
 │ alertas · despliegues         │
 │ secretos (age)                │
 └───────────────────────────────┘
              ▲  gRPC sobre mTLS, puerto 443
              │  (conexión abierta por el agente)
   ┌──────────┼──────────────┐
   │          │              │
 agente    agente          agente       ← uno por VPS (Docker, systemd,
 vps-web   vps-db          rpi-casa        journald, apt, métricas, PTY…)
```

Decisiones clave:

- **Agentes solo de salida.** Los VPS detrás de NAT o de un cortafuegos estricto funcionan sin cambios.
- **Órdenes tipadas.** El núcleo nunca envía shell arbitrario. Envía tareas tipadas e idempotentes, y
  terminales interactivas explícitas que quedan registradas en la auditoría.
- **TLS mutuo.** Cada agente recibe su propio certificado de una CA interna al darse de alta (validez de
  90 días, renovación automática).
- **Degradación controlada.** Si cae el núcleo, contenedores y servicios siguen funcionando. El agente
  guarda 24 h de métricas y eventos y los envía al reconectar.

Detalle completo: [docs/es/arquitectura.md](docs/es/arquitectura.md). Qué puede hacer el panel con tus servidores y qué se graba: [docs/es/seguridad.md](docs/es/seguridad.md).

## Sistemas soportados

| Sistema | Agente | Núcleo | Notas |
|---|---|---|---|
| Debian 12 / 13 | MVP | MVP | Plataforma principal. Todo se prueba aquí primero. |
| Ubuntu 22.04 / 24.04 LTS | MVP | MVP | Mismo apt y systemd. |
| Raspberry Pi OS (arm64) | v1 | v1 | Compilación cruzada con Go. |
| Rocky / Alma / Fedora | Más adelante | v1 | Necesita módulos dnf y firewalld. |
| Alpine | Más adelante | v1 | Sin systemd (OpenRC). |
| Arquitecturas | amd64, arm64 | amd64, arm64 | |
| Contenedores | Docker Engine 24+ con Compose v2 | | Podman más adelante. |
| Navegadores | Chrome, Firefox, Safari, Edge recientes | | Panel adaptado al móvil para acciones rápidas. |

## Instalación

El núcleo corre en Docker en una máquina; cada servidor recibe un agente con una sola orden pegada.
Paso a paso y con solución de problemas: [docs/es/instalacion.md](docs/es/instalacion.md).

```bash
git clone https://github.com/catlinux/Talaia.git && cd Talaia
packaging/core/install.sh --hostname <dirección> --port 8443 --agent-port 8444
```

## Tecnologías

| Capa | Elección |
|---|---|
| Agente | Go 1.26 · Docker SDK · go-systemd (D-Bus) · sdjournal · gopsutil · creack/pty |
| Núcleo | Go · chi · connect-go (gRPC) · sqlc · River (cola de tareas) |
| Base de datos | PostgreSQL 16 + TimescaleDB |
| Panel web | React · TypeScript · Vite · TanStack Query/Router · Tailwind · xterm.js · Monaco · uPlot |
| i18n | i18next (web) · go-i18n (núcleo, agente, CLI) |
| Proxy inverso | Caddy (gestionado mediante su API de administración) |
| Copias | restic |
| Autenticación | Sesiones + TOTP · passkeys WebAuthn · OIDC |
| Empaquetado | GoReleaser · nfpm (.deb) · cosign |
| Pruebas | go test · testcontainers · Playwright · VMs Debian |

## Idiomas (i18n)

Talaia es multilingüe desde el primer día. Idiomas soportados:

| Código | Idioma | Estado |
|---|---|---|
| `ca` | Català | Idioma de referencia de la interfaz |
| `es` | Castellano | Obligatorio |
| `en` | English | Obligatorio, idioma de reserva |

Todo texto visible para el usuario pasa por el sistema i18n. No se acepta ningún cambio que añada texto
sin las tres traducciones. El idioma se toma del perfil del usuario, después del navegador y, si no,
se usa el inglés. Detalle: [docs/es/i18n.md](docs/es/i18n.md).

La **documentación** del proyecto se escribe en **inglés y castellano**.

## Estructura del repositorio

Esta es la estructura objetivo. Las carpetas se crean a medida que empieza cada fase.

```
Talaia/
├─ core/            # talaia-core: API, hub de agentes, tareas, alertas
├─ agent/           # talaia-agent y sus módulos
├─ proto/           # contrato gRPC compartido
├─ web/             # panel web (React)
├─ cli/             # talaiactl
├─ templates/       # catálogo de apps (compose + metadatos)
├─ packaging/       # .deb, unidades systemd, install.sh, compose.yaml del núcleo
├─ scripts/         # scripts de mantenimiento (cabeceras de licencia, versiones…)
├─ test/e2e/        # pruebas de extremo a extremo en VMs Debian
└─ docs/
   ├─ en/           # documentación en inglés
   ├─ es/           # documentación en castellano
   └─ design/       # página de la hoja de ruta y maquetas (HTML)
```

## Maquetas

Abre [`docs/design/talaia-roadmap.html`](docs/design/talaia-roadmap.html) en un navegador. Contiene la
propuesta completa del proyecto y cinco maquetas: vista de flota, servidor entero, stack, asistente de
nueva app y permisos por ámbito.

## Versiones

Talaia usa [Versionado Semántico](https://semver.org/lang/es/) `MAYOR.MENOR.PARCHE`. La versión actual
está en el archivo [`VERSION`](VERSION).

Durante el desarrollo (`0.y.z`), cada fase completada de la hoja de ruta sube la versión menor:

| Versión | Hito |
|---|---|
| 0.0.x | Definición del proyecto y documentación |
| 0.1.0 | Fase 0: cimientos |
| 0.2.0 | Fase 1: agente, conexión e inventario |
| 0.3.0 | Fase 2: contenedores y stacks |
| **0.4.0** | **Fase 3: servidor entero — MVP** |
| 0.5.0 – 0.9.0 | Fases 4 a 8 |
| **1.0.0** | **Fase 9: primera versión estable** |

Las versiones de parche (`z`) se usan para correcciones y cambios pequeños dentro de una fase.

## Flujo de trabajo

```
Windows (VS Code + Claude Code)  ──commit──▶  repositorio local
                                  ──push (aprobado por el mantenedor)──▶  GitHub
Equipo Linux de pruebas          ◀──clone / pull──  GitHub  ──▶  compilar y probar
```

- El trabajo diario se hace en un equipo Windows con VS Code y Claude Code.
- Claude Code actúa como **orquestador**; otros agentes de IA actúan como **ejecutores** (coordinados con
  Agent Relay). Normas para los agentes: [CLAUDE.md](CLAUDE.md) y [AGENTS.md](AGENTS.md).
- Se pueden hacer commits libremente. **Los push siempre los aprueba el mantenedor.**
- Los cambios se prueban en un equipo Linux que clona el repositorio desde GitHub.

Detalle: [docs/es/desarrollo.md](docs/es/desarrollo.md).

## Contribuir

Las contribuciones son bienvenidas bajo los términos de la licencia. Lee
[CONTRIBUTING.es.md](CONTRIBUTING.es.md) antes de abrir una incidencia o una pull request.

## Seguridad

No informes de vulnerabilidades en incidencias públicas. Consulta [SECURITY.es.md](SECURITY.es.md).

## Créditos y licencia

**Talaia** está desarrollado y mantenido por **CatLinux**.
Copyright (C) 2026 CatLinux.

Licencia [WNCL-1.0](LICENSE): puedes usarlo, estudiarlo, modificarlo y compartirlo gratis con fines no comerciales. Venderlo o cualquier uso comercial requiere permiso por escrito.

Las copias y los forks deben conservar esta atribución y enlazar a https://github.com/catlinux/Talaia.

### Material de terceros

Este proyecto no contiene material de terceros.
