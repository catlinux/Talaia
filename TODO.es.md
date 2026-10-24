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
- [x] Verificar `packaging/core/install.sh` y la descarga del agente desde el núcleo de principio a fin en una máquina limpia

## Fase 2 · Lo que queda (ver `.agents/plans/fase2/`)

- [x] Terminal en contenedor, paso 6 (lo que falta): prueba con un Docker real en el Debian (la documentación está en `docs/es/seguridad.md`)
- [x] Terminal en contenedor, paso 7: revisión final de seguridad de todo el diff (Opus)
- [x] Pantalla de stack (maqueta 3), registros en directo (`-f`) y una nota fija en el editor de stacks: un compose se ejecuta como root en el servidor
- [x] Un stack adoptado cuya carpeta ha desaparecido se queda como una entrada rota: detectarlo y ofrecer **Dejar de gestionar** (o limpiar el registro) en el panel; encontrado en la prueba real del 22-10-2026
- [x] Cerrar la fase contra su criterio; al cerrarla la versión pasa a 0.3.0 (política del mantenedor, 2026-10-21)

## Fase 3 · Servidor entero (0.4.0, ver `.agents/plans/fase3/fase3.PLAN.md`)

- [x] Contrato para apt, systemd, journald, reinicio y terminal del host
- [x] apt, solo lectura: actualizaciones pendientes y de seguridad, paquetes retenidos, «reinicio necesario» (agente, core, pestaña Actualizaciones, insignias en Flota)
- [x] Actualizar con apt (todo o solo seguridad) de forma que sobreviva a un reinicio del agente, y reiniciar escribiendo el nombre del servidor
- [x] systemd: estado de activación, iniciar/parar/reiniciar/recargar/activar/desactivar con confirmación, pestaña Servicios
- [x] journald: filtros (unidad, prioridad, arranque, fechas, texto) y seguimiento, pestaña Registros
- [x] Terminal del host: shell de root con un código TOTP nuevo, grabación y franja roja
- [x] Pantalla de servidor con pestañas y resumen (maqueta 2) y pantalla de auditoría
- [x] Documentación EN/ES, revisión de seguridad (Opus), prueba real en dos servidores y cierre en 0.4.0 (cerrada por el mantenedor, 2026-10-22; la semana sin SSH sigue en el uso diario)

## Fase 4 · Usuarios, permisos y auditoría (0.5.0, ver `.agents/plans/fase4/fase4.PLAN.md`)

- [x] Modelo de permisos: roles fijos (administrador, operador de servidor, operador de app, lector, solo métricas) sobre organización → grupo → servidor → stack → contenedor; una asignación de stack nunca llega al host
- [x] Usuarios, equipos, grupos de servidores e invitaciones por enlace (2FA obligatorio para todos)
- [x] Permisos comprobados en cada ruta de la API, con una prueba que falla si una ruta no tiene ninguno; listas filtradas
- [x] Claves de API con ámbito (nunca terminales, usuarios ni reinicio) y `talaiactl` v1
- [x] Inicio de sesión OIDC para usuarios existentes (correo verificado), con segundo factor igualmente
- [x] Panel: pantalla Usuarios y permisos (maqueta 5), claves de API, ajustes OIDC, lista y descarga de grabaciones de terminal
- [x] Grabación de sesiones de terminal (hecha en las fases 2 y 3)
- [ ] Tras una actualización, el panel sigue mostrando la versión anterior: que se recargue solo (o avise de que hay que recargar) cuando el core se reinicie con una versión nueva
- [ ] Docs EN/ES, revisión de seguridad (Opus), prueba real con un segundo usuario, cierre en 0.5.0

## Bloque de operación (decidido el 2026-10-20, antes de cerrar la fase 2)

**Prioridad ahora (mantenedor, 2026-10-20):** las actualizaciones desde el panel y el acceso externo, para seguir actualizando y gestionando desde fuera de casa. Diseño y pasos: [`.agents/plans/operacion/operacion.PLAN.md`](.agents/plans/operacion/operacion.PLAN.md).

**Principio:** Talaia se instala desde GitHub en máquinas que no conocemos, así que no puede depender de lo que ya haya: ni de un proxy inverso, ni de que 80/443 estén libres, ni de un servicio de VPN de terceros.

- [x] Dos puertos propios, elegidos libres por el instalador (8443 y 8444 por defecto, nunca 80/443 salvo que estén libres y se pida): uno para el panel y otro para los agentes
- [x] Instalador que comprueba los puertos, pregunta el nombre público y escribe `.env`
- [x] Certificado de confianza para el panel por ACME DNS-01 dentro del core (lego; primero OVH), en cualquier puerto; los agentes siguen fijando la CA propia de Talaia
- [x] Detección de actualizaciones (último commit de `main` con el CI en verde) y API
- [x] Ayudante del host `talaia-hostd` (systemd) que trae los cambios, reconstruye, comprueba `/healthz` y vuelve atrás con una copia de la base de datos; el core nunca recibe el socket de Docker
- [x] Pantalla de Ajustes con la versión y el botón de actualizar; ca/es/en
- [x] Documentación EN/ES (VPS con otros servicios, credencial de OVH con derechos mínimos, actualizaciones)
- [x] Revisión de seguridad (Opus)
- [x] Una petición que el ayudante nunca recoge (ayudante parado o no instalado) deja el panel en «actualización en curso»: avisar de una petición de hace más de unos minutos como atascada
- [x] El ayudante no se actualiza solo: una actualización que cambie `packaging/core/hostd/` necesita volver a ejecutar `install.sh`; hacerlo desde el ayudante o avisar en el panel
- [x] Instalación real en `talaia.invierteconcriterio.es` con el mantenedor (desde cero; el Debian de casa pasa a ser un servidor más)
- [ ] Más adelante: cambiar la dirección del core en los agentes con una orden tipada (para mudarse al dominio y VPS nuevos)

## Ideas (sin planificar)

Apunta aquí las ideas nuevas en lugar de añadirlas a la fase actual.

- App móvil (PWA) con notificaciones push
- Integración con Tailscale/Headscale para crear la red privada automáticamente
- Panel de costes por proveedor
- Opción futura: Headscale u otra VPN, para dejar privados el panel o algunas máquinas (el mantenedor decidió el 2026-10-20 dejarlo para más adelante). Aparcado por ahora: Headscale pide el puerto 443 en producción, lo que rompe el principio de arriba, y un core público ya llega a servidores detrás de NAT porque los agentes conectan hacia fuera
- Opción de actualizar los agentes automáticamente tras actualizar el core (opcional, de uno en uno y parando en el primer fallo), en vez del paso aparte «actualizar todos los agentes»
- Redirección opcional de HTTP al HTTPS del panel, para que escribir el host sin `https://` ni el puerto lleve al panel
