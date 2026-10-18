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

### Entorno de pruebas
- [ ] Dos VPS desechables: Debian 12 y Debian 13
- [ ] Guía de clonar y ejecutar en el equipo Linux

## Ideas (sin planificar)

Apunta aquí las ideas nuevas en lugar de añadirlas a la fase actual.

- App móvil (PWA) con notificaciones push
- Integración con Tailscale/Headscale para crear la red privada automáticamente
- Panel de costes por proveedor
