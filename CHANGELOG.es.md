# Registro de cambios

[Read in English](CHANGELOG.md)

Todos los cambios relevantes del proyecto se documentan en este archivo.

El formato se basa en [Keep a Changelog](https://keepachangelog.com/es-ES/1.1.0/) y el proyecto sigue el
[Versionado Semántico](https://semver.org/lang/es/). La versión actual está en el archivo
[`VERSION`](VERSION).

Secciones de cada versión: **Añadido**, **Cambiado**, **Obsoleto**, **Eliminado**, **Corregido**,
**Seguridad**.

## [Sin publicar]

### Añadido

- Esqueleto del monorepo: workspace de Go, módulos `core`, `agent` y `cli` vacíos, `Makefile` y `scripts/bump-version.sh`.
- Contrato del agente `proto/agent.proto` v0 (`Heartbeat`, `Inventory`, `Metrics`) con la configuración de `buf`.
- Esqueleto del core: servidor HTTP con `/healthz`, configuración por TOML y variables de entorno, conexión a PostgreSQL y primera migración.
- Esqueleto del panel web: Vite + React + TypeScript, tokens de diseño con tema claro/oscuro, i18next (ca/es/en) con selector de idioma y pantalla «Acerca de».
- Enlace mínimo del agente: el hub del core acepta el stream del agente (HTTP/2 sin cifrar por ahora) y el agente envía inventario, latidos y métricas de CPU, leyendo `/etc/talaia/agent.toml`.
- i18n para los componentes Go (go-i18n en core y CLI, ca/es/en) y `scripts/i18n-check.mjs`, que falla si falta una clave en algún idioma.
- `make dev`: entorno de desarrollo local (PostgreSQL + TimescaleDB en Docker Compose, core, agente y servidor de desarrollo del panel).
- CI en GitHub Actions: compilación, vet y pruebas de Go, golangci-lint, buf lint y comprobación del código generado, compilación, lint, formato y pruebas del panel, y la comprobación de i18n.

### Cambiado

- Decisiones de la fase 0 cerradas: PostgreSQL + TimescaleDB, React + TypeScript, Go 1.26, Node.js 24 LTS con pnpm.

## [0.0.1] - 2026-10-18

### Añadido

- Definición del proyecto: nombre, objetivos, dos niveles de gestión (contenedor y servidor entero).
- README en inglés y castellano.
- Hoja de ruta con 10 fases y correspondencia de versiones (ROADMAP, EN/ES).
- Lista TODO de la definición del proyecto y la fase 0 (EN/ES).
- Guías CONTRIBUTING y SECURITY (EN/ES).
- Guías de arquitectura, i18n y desarrollo en `docs/en/` y `docs/es/`.
- Página de la hoja de ruta con cinco maquetas en `docs/design/talaia-roadmap.html`.
- Instrucciones para agentes de IA: `CLAUDE.md` (orquestador), `AGENTS.md` (ejecutores).
- Configuración del espacio de trabajo de VS Code, extensiones recomendadas, `.editorconfig`,
  `.gitattributes` y `.gitignore`.
- Licencia WNCL-1.0 aplicada: archivo `LICENSE` y sección de créditos en los README.

[Sin publicar]: https://github.com/catlinux/Talaia/compare/v0.0.1...HEAD
[0.0.1]: https://github.com/catlinux/Talaia/releases/tag/v0.0.1
