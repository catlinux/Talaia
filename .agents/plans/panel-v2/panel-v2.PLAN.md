# Bloque «Panel v2»: panel profesional y alertas cómodas

Estado: **plan escrito (2026-10-30, Opus).** Paso 0 (maqueta) ✅ aprobado por el mantenedor el 2026-10-30 («probemos»). Se intercala en la fase 6 entre el paso 4 y el 5
(decisión del mantenedor, 2026-10-30): el panel de Apps (pasos 11-13 de fase 6) se construirá ya sobre este diseño.

Petición del mantenedor: *«nada de la típica aplicación hecha por IA con siempre el mismo estilo. Profesional, claro y
funcional. Que gestionar mis dockers sea fácil, que vea en todo momento el estado de mis servidores y recursos, si debo
actuar sobre algo, y que añadir o quitar cosas (servicios, usuarios…) sea fácil. Esto es para facilitar, no para
complicar.»* Referencias de tono: consolas de Hetzner Cloud, DigitalOcean, Portainer y Grafana.

## Diagnóstico del panel actual (0.6.4, capturas del 2026-10-30)

- **Aspecto «hecho por IA»**: fondo con puntos y resplandores, degradados en tarjetas, borde superior de color en cada
  tarjeta, etiquetas en mayúsculas monoespaciadas con espaciado (`SERVIDORES EN LÍNEA`), titulares grandes de
  display, mucho aire. Poca información por pantalla: con 6 servidores solo se ven 4 tarjetas.
- **Navegación**: sin URLs (`App.tsx` cambia de pantalla con estado): no hay enlaces, el botón Atrás sale del panel,
  recargar vuelve a Flota. El idioma y el tema ocupan media barra lateral. No hay vista global de contenedores ni de
  stacks: para encontrar un contenedor hay que saber en qué servidor está.
- **Alertas** (la queja principal): tarjetas grandes sin filtros, sin búsqueda, sin selección ni acciones en bloque, sin
  agrupar; no se distingue lo nuevo de lo ya visto; no hay detalle (qué regla, cuándo empezó, qué hacer); las reglas,
  canales y comprobaciones viven en *Ajustes › Alertas*, lejos de las alertas.
- **Móvil**: la flota se desborda en horizontal; la navegación ocupa media pantalla.

## Principios de diseño (obligatorios en todos los pasos)

1. **Primero el estado y lo que pide acción.** Cada pantalla responde arriba a «¿va todo bien? ¿qué tengo que hacer?».
2. **Densidad de consola, no de folleto**: tablas antes que tarjetas para listas; fila de 40 px; base 14 px.
3. **Sobriedad**: superficies planas, borde de 1 px, radio 6 px, sombra solo en capas flotantes (menús, cajones,
   diálogos). Prohibido: fondos con puntos o resplandores, degradados, bordes de color decorativos, etiquetas en
   mayúsculas espaciadas, iconos decorativos en KPI, animaciones de entrada.
4. **Tipografía**: IBM Plex Sans para todo; JetBrains Mono solo para identificadores técnicos (imágenes, IDs, puertos,
   rutas, versiones). Familjen Grotesk solo en el logotipo. Títulos de página 20 px semibold, de sección 15 px.
5. **Color con significado**: neutros (gris pizarra) + un acento (el turquesa actual) para acciones y selección. Verde,
   ámbar, rojo y gris solo para estado, siempre con punto **y** texto (nunca solo color).
6. **Vocabulario de estado único** en todo el panel: `Correcto · Aviso · Crítico · Sin conexión · Desconocido`.
7. **Cada lista** tiene búsqueda, filtros y orden; estado vacío con la acción principal; esqueleto al cargar; error con
   «Reintentar». Los filtros viven en la URL.
8. **Cada acción destructiva** pide confirmación diciendo qué pasará; cada acción termina con un aviso (toast) de
   resultado. Las acciones frecuentes están a un clic desde la fila (menú «⋯»).
9. **URLs reales** (`/servers/:id/containers`, `/alerts?severity=critical`): Atrás, recargar y compartir funcionan.
   El core ya sirve `index.html` en rutas desconocidas (`core/internal/api/static.go`).
10. **Teclado**: `Ctrl/⌘+K` paleta de comandos, `/` enfoca la búsqueda, `Esc` cierra cajones y diálogos.
11. Sin dependencias npm nuevas (router, tabla y paleta se escriben en el repo; son pequeños). i18n ca/es/en en todo.
    Claro, oscuro y móvil (390 px sin desbordes) en cada paso, con capturas.

## Estructura nueva

- **Barra lateral** (plegable; cajón en móvil), agrupada:
  - *Resumen*
  - **Infraestructura**: Servidores · Contenedores · Stacks · (Apps, Dominios y Plantillas llegan en fase 6)
  - **Supervisión**: Alertas (con contador) · Auditoría
  - **Administración**: Usuarios · Ajustes
- **Barra superior**: migas de pan · búsqueda/paleta (`Ctrl+K`) · campana con alertas abiertas (desplegable con las 5
  últimas) · menú de usuario (idioma, tema, Acerca de, cerrar sesión).
- **Resumen** (`/`): franja de estado (servidores en línea, alertas críticas/aviso, actualizaciones de seguridad,
  reinicios pendientes) → **«Requiere atención»** (lista priorizada y accionable: alerta crítica, servidor sin
  conexión, disco > 85 %, servicio caído, reinicio pendiente, agente desactualizado; cada fila con su botón) → tabla
  compacta de servidores con CPU/RAM/disco en barras finas → actividad reciente.
- **Servidores** (`/servers`): tabla (estado, nombre, grupo, SO, CPU, RAM, disco, contenedores, alertas,
  actualizaciones, agente) con búsqueda, filtros por estado/grupo/etiqueta y orden; vista de tarjetas opcional
  (recordada por navegador). Acción principal: *Añadir servidor*.
- **Ficha de servidor** (`/servers/:id/:tab`): cabecera fija con estado, métricas clave y menú de acciones (terminal,
  reiniciar, actualizar agente, quitar); pestañas como rutas.
- **Contenedores** (`/containers`): todos los contenedores de la flota en una tabla (estado, nombre, imagen, stack,
  servidor, CPU, RAM, desde) con filtros y acciones por fila (iniciar, parar, reiniciar, logs, terminal) y selección
  para acciones en bloque (reiniciar/parar varios, con confirmación). **Stacks** (`/stacks`) igual a nivel de stack.
- **Alertas** (`/alerts/:tab`): pestañas *Bandeja · Historial · Silencios · Reglas · Canales · Comprobaciones*
  (las tres últimas se mudan desde Ajustes, según permisos).
  - **Bandeja**: tabla con casilla, gravedad, qué pasa, dónde (servidor › stack › contenedor, enlazados), desde
    cuándo, estado (*Nueva* en negrita / *Vista por X* / *Silenciada hasta…*). Filtros: gravedad, estado, servidor,
    tipo de regla, texto. Agrupar por *nada / servidor / regla*. Acciones en bloque: confirmar, silenciar
    (1 h / 4 h / 24 h / hasta…), dejar de vigilar.
  - **Detalle** (cajón lateral, `/alerts/:id`): mensaje, cronología (empezó, confirmada, silenciada, resuelta),
    regla que la disparó con umbral y enlace para editarla, y **acciones directas sobre la causa**: ver logs o
    reiniciar el contenedor, abrir el servidor, ver la gráfica de la métrica en la ventana de la alerta.

## Pasos

Cada paso sube el parche siguiente libre (`bash scripts/bump-version.sh 0.6.N`, con la entrada en *Sin publicar* de
los dos CHANGELOG antes) y se marca ✅ aquí y en los dos TODO. Ejecutor: AgentRelay, uno a la vez, **sin editar el
repo mientras corre**; el orquestador ejecuta `pnpm test`, `pnpm lint`, `pnpm exec tsc -b`, `node scripts/i18n-check.mjs`
y hace las capturas (arnés temporal: memoria `handoff.md`, «Capturas»). Cada brief incluye en `references` este plan y
`docs/design/panel-v2.html`.

| # | Paso | Archivos principales | Terminado cuando | Ejecutor · esfuerzo |
|---|---|---|---|---|
| 0 | Maqueta estática del diseño (shell, Resumen, Servidores, Alertas + detalle; claro/oscuro/móvil) y aprobación del mantenedor | `docs/design/panel-v2.html` | el mantenedor la aprueba (o pide cambios) | Opus (sesión) |
| 1 | ✅ Tokens y base: paleta, tipografía, espaciado, radios; quitar fondos, resplandores, degradados y etiquetas espaciadas en todo `styles/` | `web/src/styles/{tokens,global}.css` + limpieza en el resto de `styles/*.css` | capturas de todas las pantallas sin decoraciones; tests verdes | AgentRelay · medio |
| 2 | ✅ Componentes comunes: `PageHeader`, `DataTable` (orden, filtros, selección, vacío/carga/error, tarjetas en móvil), `StatusBadge`, `Drawer`, `Menu`, `Toast`, `ConfirmDialog`, `FilterBar` | `web/src/components/ui/**`, `web/src/styles/ui.css` | tests de cada componente (teclado, a11y básica) | AgentRelay · alto |
| 3 | ✅ Router propio (History API, rutas con parámetros y query) y shell nuevo: barra lateral agrupada y plegable, barra superior (migas, campana, menú de usuario con idioma/tema/acerca de/salir), cajón en móvil | `web/src/router.tsx`, `web/src/App.tsx`, `web/src/components/shell/**`, `web/src/styles/shell.css`, locales | Atrás/recargar/enlace directo funcionan en todas las pantallas; tests de rutas y permisos | AgentRelay · alto |
| 4 | ✅ Resumen: franja de estado, «Requiere atención» accionable, tabla compacta, actividad | `web/src/screens/Overview*.tsx`, `web/src/screens/overview/**`, locales | capturas; tests de prioridad de los elementos de atención | AgentRelay · alto |
| 5 | Servidores: tabla con filtros/orden, vista de tarjetas opcional, *Añadir servidor* | `web/src/screens/Servers*.tsx` (sustituye `Fleet.tsx`), `web/src/screens/fleet/**` | capturas; tests de filtros, orden y permisos | AgentRelay · medio |
| 6 | Ficha de servidor: cabecera fija con métricas y menú de acciones; pestañas como rutas; partir `ServerScreen.tsx` (1100 líneas) | `web/src/screens/ServerScreen.tsx`, `web/src/screens/server/**` | sin cambios de funcionalidad; tests existentes verdes | AgentRelay · alto |
| 7 | Contenedores y Stacks globales: tablas de toda la flota con acciones por fila y en bloque | `web/src/screens/{Containers,Stacks}*.tsx`, `web/src/api/containers.ts` (solo si falta un listado), locales | capturas; tests de acciones y permisos por ámbito | AgentRelay · alto |
| 7b | Catálogo «¿qué es esto?» (petición del mantenedor, 2026-10-30): ficha de ~60-80 servicios de Debian y de las imágenes Docker más comunes con qué es, categoría, importancia (esencial/importante/prescindible) y qué hacer si falla; icono ⓘ con tooltip en las tablas de contenedores, stacks y servicios y ficha completa en el detalle; lo desconocido muestra su origen (stack/servicio de Compose, imagen, descripción de systemd); la importancia pondera «Requiere atención» | `web/src/catalog/**`, `web/src/components/ui/InfoTip*.tsx`, pantallas de contenedores/servicios, locales | tests de búsqueda en el catálogo (nombres de unidad, imágenes con registro y etiqueta) y del tooltip | AgentRelay · medio |
| 8 | Alertas · Bandeja: tabla, filtros en la URL, agrupar, nueva/vista/silenciada, acciones en bloque | `web/src/screens/Alerts.tsx`, `web/src/screens/alerts/{Inbox,BulkBar,Filters}*.tsx`, locales | tests: filtros, agrupación, bloque con permisos parciales | AgentRelay · alto |
| 9 | Alertas · Detalle en cajón con cronología, regla y acciones sobre la causa; campana de la barra superior | `web/src/screens/alerts/AlertDrawer*.tsx`, `web/src/components/shell/AlertBell*.tsx`, locales | tests; capturas | AgentRelay · alto |
| 10 | Mudar Reglas, Canales y Comprobaciones de Ajustes a Alertas (pestañas por permiso); Ajustes queda con cuenta, servidores, integraciones y sistema | `web/src/screens/alerts/**`, `web/src/screens/Settings.tsx`, `web/src/screens/settings/SettingsTabs.tsx`, locales | sin pérdida de funciones; tests movidos y verdes | AgentRelay · medio |
| 11 | Paleta de comandos (`Ctrl+K`): ir a servidor/contenedor/stack/pantalla y acciones frecuentes | `web/src/components/shell/CommandPalette*.tsx`, locales | tests de búsqueda y teclado | AgentRelay · medio |
| 12 | Usuarios, Auditoría y Ajustes con los componentes nuevos (tablas, cabeceras, diálogos) | `web/src/screens/{Users,Audit,Settings}*.tsx` y subcarpetas | capturas; tests verdes | AgentRelay · medio |
| 13 | Repaso móvil y accesibilidad, capturas finales, docs EN+ES (guía del panel), revisión Opus del bloque | `docs/{en,es}/**`, ajustes menores | 390 px sin desbordes; revisión hecha | Opus |

Orden: 0 → 1 → 2 → 3, luego 4-12 en cualquier orden salvo 7b tras 7, 9 tras 8 y 10 tras 8. Después se retoma la fase 6 en su
paso 5.

## Fuera del bloque (a *Ideas* si surge)

- Notas propias por contenedor/servicio (necesitan tabla y API en el core): anotadas en *Ideas* del TODO.

Paneles personalizables, widgets arrastrables, notificaciones del navegador, cambios en la API del core (si un paso
necesita un endpoint nuevo, el ejecutor para y lo anota aquí).
