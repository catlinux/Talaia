# TODO

[Read in English](TODO.md) · [Hoja de ruta](ROADMAP.es.md)

Lista de tareas a corto plazo. La [hoja de ruta](ROADMAP.es.md) describe las fases; este archivo sigue las
tareas concretas de la fase actual y de la siguiente.

**Norma para agentes y colaboradores:** cuando termines una tarea, márcala aquí **y** en
[TODO.md](TODO.md) en el mismo commit.

---

## Ahora · Definición del proyecto (0.0.x)

- [x] Crear el repositorio de GitHub `catlinux/Talaia`
- [x] README (EN/ES)
- [x] Hoja de ruta (EN/ES)
- [x] TODO (EN/ES)
- [x] CHANGELOG (EN/ES)
- [x] CONTRIBUTING y SECURITY (EN/ES)
- [x] Instrucciones para agentes: `CLAUDE.md` (orquestador) y `AGENTS.md` (ejecutores)
- [x] Guías de arquitectura, i18n y desarrollo (EN/ES)
- [x] Página de la hoja de ruta y maquetas en `docs/design/`
- [x] Configuración del espacio de trabajo de VS Code y extensiones recomendadas
- [ ] Poner la descripción, web y temas del repositorio en GitHub
- [x] Aplicar la licencia WNCL-1.0 con el script del mantenedor (archivo LICENSE y cabeceras)
- [ ] Configurar Agent Relay (orquestador + ejecutores) en el equipo Windows
- [ ] Preparar el equipo Linux de pruebas (Debian 13, Git, Docker, Go)
- [ ] Activar el aviso privado de vulnerabilidades de GitHub

## Siguiente · Fase 0: Cimientos (0.1.0)

### Decisiones a cerrar
- [x] Base de datos: PostgreSQL + TimescaleDB (SQLite descartada: la cola de tareas y el historial de métricas encajan mejor en Postgres)
- [x] Framework web: React + TypeScript (SvelteKit descartado: menos ecosistema de componentes de terminal y editor)
- [x] Versión mínima de Go: 1.26
- [x] Node.js 24 LTS y pnpm (fijado con Corepack mediante el campo `packageManager`)

### Repositorio y CI
- [x] Crear carpetas: `core/`, `agent/`, `proto/`, `web/`, `cli/`, `packaging/`, `test/e2e/`
- [x] Workspace de Go (`go.work`) con `core`, `agent`, `cli`
- [x] `Makefile` con `dev`, `build`, `test`, `lint`, `i18n-check`, `license-check`
- [ ] GitHub Actions: compilación, pruebas, lint, comprobación i18n, comprobación de licencia
- [x] `scripts/bump-version.sh` que actualice `VERSION` y los dos changelogs

### Contrato
- [x] `proto/agent.proto` v0: `Heartbeat`, `Inventory`, `Metrics`
- [ ] Generación de código con `buf`

### Núcleo
- [x] Servidor HTTP con `/healthz`
- [x] Conexión a PostgreSQL y primera migración
- [x] Archivo de configuración y variables de entorno

### Agente
- [x] Binario mínimo que se conecte al núcleo y envíe el hostname y la CPU cada 15 s
- [x] Configuración en `/etc/talaia/agent.toml`

### Panel web
- [x] Proyecto Vite + React + TypeScript
- [x] Tokens de diseño y tema claro/oscuro de las maquetas
- [x] i18next con `ca`, `es`, `en` y selector de idioma
- [x] Pantalla «Acerca de» con la atribución «Talaia — created by CatLinux» (autoría de CatLinux)

### i18n
- [x] Estructura de archivos de idioma y convención de claves (ver [docs/es/i18n.md](docs/es/i18n.md))
- [x] Script que falle si falta alguna clave en `ca`, `es` o `en`
- [x] go-i18n preparado para los mensajes del núcleo y la CLI (el agente no muestra texto a los usuarios)

### Interfaz
- [ ] Reconocer las tipografías incluidas (Familjen Grotesk, IBM Plex Sans y JetBrains Mono, todas SIL OFL 1.1) en la sección de terceros del README (mantenedor: los créditos los apruebas tú)

### Entorno de pruebas
- [ ] Dos VPS desechables: Debian 12 y Debian 13
- [x] Guía de clonar y ejecutar en el equipo Linux (`docs/es/instalacion.md`)
- [ ] Verificar `packaging/core/install.sh` y la descarga del agente desde el núcleo de principio a fin en una máquina limpia

## Fase 2 · Lo que queda (ver `.agents/plans/fase2/`)

- [ ] Terminal en contenedor, paso 6: documentación EN/ES (seguridad: el administrador es root en los servidores; grabación y retención; `terminal export`) y prueba con un Docker real en el Debian
- [ ] Terminal en contenedor, paso 7: revisión final de seguridad de todo el diff (Opus)
- [ ] Pantalla de stack (maqueta 3), registros en directo (`-f`) y una nota fija en el editor de stacks: un compose se ejecuta como root en el servidor
- [ ] Cerrar la fase contra su criterio; versión 0.3.0 solo si el mantenedor lo pide

## Bloque de operación (decidido el 2026-10-20, antes de cerrar la fase 2)

**Prioridad ahora (mantenedor, 2026-10-20):** las actualizaciones desde el panel y el acceso externo, para seguir actualizando y gestionando desde fuera de casa.

Primero el diseño con un modelo potente (Opus), escrito en `.agents/plans/operacion/operacion.PLAN.md`; después se ejecuta con uno más barato.

### Actualizaciones desde el panel
- [ ] Ayudante del host `talaia-hostd` (systemd, root) con una lista cerrada de operaciones. El core solo escribe ficheros de petición en su volumen de datos; nunca recibe el socket de Docker (el contenedor es de solo lectura y sin privilegios a propósito)
- [ ] Detectar: el core pregunta a GitHub por el último commit de `main` cada ~30 minutos (peticiones condicionales), lo compara con el commit grabado en su imagen al construirla y ofrece solo commits con el CI en verde
- [ ] Aplicar: `git pull --ff-only` y `docker compose up -d --build`, esperar a `/healthz` y volver al commit anterior si no responde (las migraciones de la base de datos solo avanzan: dejarlo dicho en el plan)
- [ ] Pantalla de Ajustes (todavía no existe): tarjeta de versión, «N commits nuevos», botón de actualizar con confirmación, progreso y resultado; ca/es/en
- [ ] Documentación: instalar el ayudante una vez en el Debian con `sudo`; después no hacen falta órdenes
- [ ] Más adelante: actualizar desde el core los agentes de los demás servidores (hoy: `server upgrade-command` pegado a mano)

### Acceso remoto y HTTPS de confianza (decidido por el mantenedor el 2026-10-20: core, panel y Headscale en el VPS)
- [ ] Prioritario junto con las actualizaciones: seguir actualizando y gestionando desde fuera de casa
- [ ] Instalar el core, el panel y Headscale en el VPS con IP fija, en `talaia.invierteconcriterio.es`. Instalación desde cero: el core de casa era solo de pruebas, así que el Debian de casa pasa a ser un servidor más y se le vuelve a instalar el agente. Headscale va instalado directamente y no en un contenedor (sus mantenedores no recomiendan contenedores ni proxies inversos: comprobar la documentación actual)
- [ ] Ese VPS ya aloja muchas otras webs, así que primero hay que averiguar qué proxy tiene los puertos 80 y 443 (nginx, Caddy, Traefik; en el host o en Docker). El mTLS de los agentes tiene que llegar al core sin que ese proxy termine el TLS: elegir en el diseño entre un puerto aparte para los agentes, paso directo por nombre, o el core en su propio puerto
- [ ] DNS en OVH: un registro A público `talaia.invierteconcriterio.es` hacia el VPS y un certificado de Let's Encrypt (DNS-01 mediante la API de OVH con una credencial limitada a la zona, o HTTP-01 si el proxy existente lo permite)
- [ ] Core: presentar el certificado válido a los navegadores y el de su propia CA a los agentes, según el nombre que pidan; renovar y recargar el certificado sin reinicios (desde el ayudante del host)
- [ ] Headscale y la VPN sirven para llegar a máquinas sin IP pública (el Debian de casa) y para el acceso privado; el panel público no depende de ellos
- [ ] El nombre no puede estar fijado en ningún sitio: más adelante el mantenedor comprará un dominio nuevo y un VPS más potente. Dejar escrita y probada la mudanza (nombre nuevo en `TALAIA_PUBLIC_HOSTNAMES`, certificado nuevo, agentes apuntados a la dirección nueva o conservados gracias a la CA, volumen de datos copiado)
- [ ] Documentación EN/ES: el core sigue sin depender de una red concreta; sustituir la sección 6 de la guía de instalación, que está sin probar

## Ideas (sin planificar)

Apunta aquí las ideas nuevas en lugar de añadirlas a la fase actual.

- App móvil (PWA) con notificaciones push
- Integración con Tailscale/Headscale para crear la red privada automáticamente
- Panel de costes por proveedor
