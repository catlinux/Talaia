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
- Código QR en la pantalla de configuración del segundo factor, dibujado como SVG en línea para que funcione con la CSP del panel.
- El core sirve `install.sh` y los paquetes del agente (amd64 y arm64, construidos dentro de su imagen) y `token create` imprime la orden de instalación completa, de modo que un servidor nuevo necesita una sola línea pegada y ningún paquete copiado a mano. `install.sh` comprueba la huella de la CA antes de fiarse del paquete, y `--deb` pasa a ser opcional.
- Nuevos ajustes `TALAIA_DIST_DIR` y `TALAIA_PUBLIC_PORT` (`dist_dir`, `public_port`); el puerto público también alimenta los orígenes permitidos por defecto, que ahora funcionan con cualquier puerto publicado.
- `packaging/core/install.sh`: prepara el núcleo con una sola orden (contraseña aleatoria de base de datos en `.env`, construcción, arranque, administrador), y la guía de instalación `docs/es/instalacion.md`.
- Trabajo de CI `core stack (docker)`: ejecuta el instalador del núcleo y comprueba que sirve el panel, la CA, `install.sh` y los dos paquetes del agente, que `token create` imprime la orden de instalación, y que un servidor instala su agente con ella y se da de alta.
- API `POST /api/v1/servers/enrollment-tokens` (sesión completa): crea un token de alta de un solo uso y 15 minutos para un nombre de servidor y devuelve la orden de instalación; se audita como `enrollment_token_created`.
- `talaia-core server list` imprime los servidores dados de alta con sus ids, que necesita `server revoke`.
- La orden de instalación incluye ahora el SHA-256 de `install.sh` y se niega a ejecutar un script que no coincida, de modo que una máquina intermedia no puede cambiar el script que se descarga antes de confiar en la CA.
- Botón «Añadir servidor» en la pantalla de flota: pide un nombre, crea el token de un solo uso y muestra la orden de instalación con un botón de copiar, en ca/es/en. El mensaje de flota vacía apunta ahora a él.
- Enlace mínimo del agente: el hub del core acepta el stream del agente (HTTP/2 sin cifrar por ahora) y el agente envía inventario, latidos y métricas de CPU, leyendo `/etc/talaia/agent.toml`.
- i18n para los componentes Go (go-i18n en core y CLI, ca/es/en) y `scripts/i18n-check.mjs`, que falla si falta una clave en algún idioma.
- `make dev`: entorno de desarrollo local (PostgreSQL + TimescaleDB en Docker Compose, core, agente y servidor de desarrollo del panel).
- CI en GitHub Actions: compilación, vet y pruebas de Go, golangci-lint, buf lint y comprobación del código generado, compilación, lint, formato y pruebas del panel, y la comprobación de i18n.
- Métricas en vivo del agente (CPU, RAM, disco raíz, contadores de red), que guarda el hub del core.
- Paquete `.deb` del agente (nfpm), unidad systemd e `install.sh` (`scripts/build-deb.sh`). El enrolamiento aún no está implementado.
- Pantalla de flota (maqueta 1) con datos de ejemplo tras una interfaz de cliente de API, y navegación Flota/Acerca de.
- Inventario del agente con contenedores Docker, proyectos Compose y servicios systemd, reenviado solo cuando cambia.
- Base del enrolamiento en el core: CA interna (ECDSA P-256), tablas `server` y `enrollment_token`, y los comandos `talaia-core token create` y `talaia-core server revoke`.
- Core sobre TLS 1.3 con autenticación mutua: `GET /ca.pem`, `POST /enroll`, `POST /renew`, identidad del agente tomada del certificado de cliente y modo `insecure_dev` sin cifrar para desarrollo.
- Enrolamiento del agente con token de un solo uso anclado a la huella de la CA, enlace mTLS y renovación del certificado; `install.sh` ahora recibe `--token` y `--ca-fingerprint`.

### Seguridad

- Revisión de seguridad del enrolamiento: la revocación cierra los streams abiertos, la identidad del agente queda ligada al número de serie del certificado actual o anterior, el enrolamiento es una sola transacción, la clave y el certificado del agente se guardan en un único archivo atómico, `insecure_dev` solo escucha en loopback, los errores permanentes del agente detienen los reinicios de systemd (salida 78) y los directorios de la CA y de los certificados se fuerzan a modo 0700.
- Autenticación del panel (adelantada de la fase 3): administrador único creado desde la consola, contraseña con argon2id, TOTP obligatorio con códigos de recuperación de un solo uso, cookie de sesión segura, protección CSRF, límites contra fuerza bruta, registro de auditoría y pantallas de acceso en ca/es/en.
- Despliegue del core con Docker Compose (`packaging/core/`): una imagen con el core y el panel web, PostgreSQL 16 + TimescaleDB, sistema de archivos de solo lectura y sin capacidades.
- `GET /api/v1/servers` (requiere sesión completa) y la pantalla de flota alimentada por él, refrescada cada 15 segundos; los datos que aún no existen se muestran como no disponibles en lugar de números de ejemplo.

### Cambiado

- Nuevo aspecto del panel: tipografías autoalojadas (Familjen Grotesk, IBM Plex Sans y JetBrains Mono, que estaban declaradas pero nunca se cargaban), marco con barra lateral que pasa a barra superior en móvil, balizas de estado, medidores segmentados, un logotipo nuevo de atalaya, un acceso rediseñado con un haz de luz y animaciones que respetan `prefers-reduced-motion`. La flota ya no muestra un panel de actividad vacío. Ninguna imagen es una URL `data:`, así que el panel funciona con su CSP estricta.
- Decisiones de la fase 0 cerradas: PostgreSQL + TimescaleDB, React + TypeScript, Go 1.26, Node.js 24 LTS con pnpm.
- La sesión que espera el segundo factor dura 15 minutos en el primer acceso (antes 5), suficiente para instalar una aplicación de autenticación y escanear el QR; quien ya tiene el segundo factor mantiene 5 minutos. La pantalla de acceso indica ahora cuándo ha caducado la sesión.

### Corregido

- El core ya no se cae al arrancar cuando `TALAIA_PUBLIC_HOSTNAMES` solo contiene direcciones IP.
- Los scripts de shell (`packaging/install.sh`, `scripts/*.sh`) se guardan ahora como ejecutables, así que `./install.sh` funciona tras clonar.

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
