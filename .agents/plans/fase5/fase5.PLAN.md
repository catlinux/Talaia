# Fase 5 · Monitorización y alertas (0.6.0)

Estado: **pasos 1 a 12 hechos; auditoría Opus hecha (0.5.14). Queda el paso 13: docs EN+ES, prueba real con el mantenedor y cierre 0.6.0.** Pruebas con BD: el Postgres portable local lleva TimescaleDB 2.23.1 (la 2.31 no carga en PG 16.11); los ayudantes de prueba usan `search_path=<esquema>,public`. Criterio del ROADMAP: *«si un contenedor se para o un disco pasa
del 90 %, te enteras en menos de un minuto»*.

Punto de partida (0.5.1): las métricas viven **solo en memoria** en el hub (`core/internal/hub/hub.go`, último valor
de `Metrics` cada 15 s y `ContainerStats` cada 15 s); no hay histórico ni TimescaleDB activo (la imagen
`timescale/timescaledb:latest-pg16` ya está en `packaging/{core,dev}/compose.yaml`, pero ninguna migración crea la
extensión). El agente manda el inventario cada **60 s** o al acabar un trabajo de Talaia, así que un contenedor que
muere por su cuenta tarda hasta 60 s en verse: **no cumple el criterio** sin cambiar el agente. `Container` no lleva
salud (healthcheck). Secretos: `core/internal/auth/secretbox.go` (AES-256-GCM, la misma clave que TOTP y OIDC).
Permisos: `core/internal/rbac` + `routePerms` con prueba guardiana `chi.Walk`. Última migración: `0013_oidc.sql`.
El panel no tiene gráficos ni librería de gráficos.

Reglas de toda la fase (CLAUDE.md §3): permisos siempre en el core; textos por i18n ca/es/en (también los de las
notificaciones); docs EN+ES; estilo según la memoria `feedback-estilo-visual`; **sin dependencias nuevas salvo que
el paso lo diga** (si se añade una: `node scripts/third-party-notices.mjs` y `CREDITS.md`/`CREDITS.es.md`; recordar al
mantenedor el script de licencias para archivos nuevos). Proto: no renumerar campos.

## Decisiones

### D1 · Histórico de métricas (TimescaleDB, solo funciones Apache-2)
- Migración `CREATE EXTENSION IF NOT EXISTS timescaledb`; si falla, el core arranca con un error claro («Talaia
  necesita PostgreSQL con TimescaleDB; usa la imagen de packaging/core/compose.yaml»).
- **Solo hypertables, `time_bucket` y `drop_chunks`** (licencia Apache-2). **Nada de continuous aggregates,
  compresión ni `add_*_policy`** (son de la licencia TSL de Timescale): el downsampling y la retención los hace el
  propio core con un bucle Go. Así Talaia funciona también con la imagen `-oss`.
- Tablas (hypertables por `time`):
  - `metric_server(time, server_id, cpu_pct, mem_used, mem_total, disk_used, disk_total, net_rx_bps, net_tx_bps)`
  - `metric_server_5m` y `metric_server_1h`: mismas columnas en `avg` + `cpu_max`, `mem_max`, `disk_max`; PK
    `(server_id, time)` para insertar idempotente (`ON CONFLICT DO NOTHING`).
  - `metric_container(time, server_id, container_name, stack, cpu_pct, mem_used, mem_limit)` y sus `_5m`/`_1h`.
    La clave es el **nombre** del contenedor (el id cambia al recrearlo).
- Las tasas de red las calcula el core con la diferencia de contadores entre dos muestras (si el contador baja, se
  descarta esa muestra: reinicio del host).
- Escritura por lotes: un `Recorder` acumula en memoria y hace `COPY` cada 15 s; si la BD falla, descarta y avisa
  en el log (nunca bloquea el hub).
- Downsampling cada 5 min de los buckets **cerrados**; retención diaria con `drop_chunks`. Por defecto (sección
  `[metrics]` del archivo de configuración del core, no en el panel): servidor crudo 48 h, 5 min 30 d, 1 h 400 d;
  contenedor crudo 24 h, 5 min 7 d, 1 h 90 d.
- API: resolución automática por rango (≤ 6 h crudo, ≤ 7 d 5 min, más 1 h), máximo ~500 puntos por serie.

### D2 · Detección rápida en el agente
- `Container` gana `health = 8` (`healthy | unhealthy | starting | ""`), sacado del `Status` de la lista de Docker
  (`"Up 2 hours (healthy)"`), sin `inspect` por contenedor.
- El agente se suscribe a los eventos de Docker (`type=container`, `start, die, stop, kill, oom, destroy,
  health_status`) y, con un rebote de 2 s, llama a `sendInventoryIfChanged`. Si el flujo de eventos se corta,
  reintenta con backoff; el tick de 60 s sigue como red de seguridad. Un agente viejo sigue funcionando (más lento).

### D3 · Reglas tipadas (sin lenguaje de consultas)
| Tipo | Sujeto | Parámetros | Por defecto (creada en la migración) |
|---|---|---|---|
| `server_offline` | servidor | segundos sin señal | sí, 45 s, crítica |
| `cpu_high` / `mem_high` | servidor | umbral %, duración | no |
| `disk_high` | servidor (raíz) | umbral %, duración | sí, 90 %, 0 s, crítica |
| `container_down` | contenedor vigilado | duración | sí, 20 s, crítica |
| `container_unhealthy` | contenedor | duración | sí, 60 s, aviso |
| `check_failed` | comprobación (D5) | fallos seguidos | sí, 2 fallos, crítica |
| `cert_expiry` | comprobación HTTPS/TLS | días | sí, 14 d, aviso |

- Cada regla tiene ámbito con el modelo de fase 4 (`organization | group | server | stack | container`), severidad
  (`warning | critical`), canales (lista vacía = **todos los canales activos**, para que el criterio funcione con
  solo dar de alta un canal) y `repeat_interval` (0 = no repetir; por defecto 0).
- **Histéresis**: las reglas de umbral se resuelven 2 puntos por debajo (90 % dispara, < 88 % resuelve).
- **Contenedores vigilados** (`alert_watch(server_id, container_name, last_running_at)`): entra todo contenedor que
  el core ve `running`. `container_down` dispara si un vigilado no está `running` (o ha desaparecido). Parar,
  bajar o borrar desde Talaia lo quita de la lista (parada esperada, sin aviso); «Dejar de vigilar» en la alerta
  también. Así sobrevive a reinicios del core sin avisos falsos.

### D4 · Motor de alertas (`core/internal/alerts`)
- Evalúa cada 10 s sobre la foto del hub y los resultados de comprobaciones, y **además al momento** cuando llega un
  inventario nuevo, se desconecta un agente o termina una comprobación.
- Estado por huella (`regla + sujeto`): `ok → pending (duración) → firing → resolved`. Tabla `alert` (incidente) con
  índice único parcial por huella abierta: nunca dos incidentes abiertos iguales.
- **Avisos repetidos**: una notificación al disparar y otra al resolver; enfriamiento de 10 min por huella para
  volver a notificar un redisparo (el incidente sí se registra); `repeat_interval` opcional; **acusar recibo**
  (`ack`) corta las repeticiones.
- **Gracia al arrancar el core**: 90 s sin `server_offline` (los agentes se están reconectando). Los incidentes
  abiertos se cargan de la BD; no se renotifican.
- **Silencios** (`silence`): ámbito + regla opcional, `ends_at` obligatorio (máx. 30 d), motivo y autor. Silenciado =
  se registra y se ve en el panel, no se notifica. **Silencio automático**: un reinicio del host lanzado desde Talaia
  silencia ese servidor 10 min.
- Todo cambio (regla, silencio, ack, canal) se audita.

### D5 · Comprobaciones HTTP/TCP/TLS (`core/internal/checks`)
- Se ejecutan **desde el core** (la vista de tus usuarios). Desde un agente (servicios internos del VPS) → *Ideas*.
- Tipos: `http` (URL, método GET/HEAD, códigos esperados, timeout, sin cuerpo guardado), `tcp` (host:puerto), `tls`
  (host:puerto, solo caducidad). Las HTTPS registran el `NotAfter` del certificado hoja para `cert_expiry`.
- Intervalo mínimo 30 s (por defecto 60 s), timeout máx. 10 s, concurrencia limitada (8). Redirecciones: máx. 3 y
  cada salto pasa por el guardián D6. Resultado en `check_result` (hypertable, retención 30 d): ok, latencia,
  código, error corto, `cert_not_after`.
- Una comprobación puede asociarse a un servidor (opcional) solo para el ámbito de permisos y de silencios.

### D6 · Guardián de salida contra SSRF (`core/internal/netguard`)
- Todo el tráfico saliente de comprobaciones y notificaciones usa un `net.Dialer` con `Control` que **valida la IP
  ya resuelta en el momento de conectar** (evita el DNS rebinding) y rechaza: loopback, `0.0.0.0/8`, privadas RFC
  1918, CGNAT `100.64/10`, enlace local `169.254/16` y `fe80::/10`, ULA `fc00::/7`, multicast, broadcast, y las
  IPv4 mapeadas en IPv6 de todo lo anterior.
- Ajuste de organización `allow_private_targets` (por defecto **no**, solo admin) para vigilar servicios de la red
  privada; **los metadatos de nube (`169.254.169.254`, `fd00:ec2::254`) siguen bloqueados siempre**.
- Solo esquemas `http`/`https`; sin proxy del entorno; respuesta leída con límite (64 KiB).

### D7 · Canales de notificación (`core/internal/notify`)
| Tipo | Público (`config`) | Secreto (sellado con SecretBox) |
|---|---|---|
| `telegram` | chat id | token del bot (host fijo `api.telegram.org`) |
| `ntfy` | URL del servidor (por defecto `https://ntfy.sh`), prioridad | tema, token opcional |
| `email` | host, puerto, de, para, modo TLS (`starttls` obligatorio o `tls` implícito; nunca en claro) | usuario y contraseña |
| `discord` | — | URL del webhook (solo hosts `discord.com`/`discordapp.com`) |
| `webhook` | — | URL; secreto HMAC opcional |

- Los secretos son de **solo escritura** en la API (`has_secret: true`); nunca vuelven al panel ni salen en logs. **Ojo**:
  los errores de `net/http` incluyen la URL (Telegram lleva el token en la ruta): redactar antes de loguear o guardar
  `last_error`.
- Webhook: JSON versionado (`{"version":1,"event":"firing|resolved",...}`), cabeceras `X-Talaia-Timestamp` y
  `X-Talaia-Signature: sha256=<HMAC(timestamp + "." + cuerpo)>`.
- Idioma por canal (`ca | es | en`, por defecto `en`), con go-i18n del core.
- **Cola de envío** `notification_delivery` (alerta, canal, evento, intentos, `next_attempt_at`, estado,
  `last_error`): reintentos con backoff (5 intentos, hasta ~15 min) y sobrevive a reinicios del core. Botón
  «Enviar prueba» por canal.

### D8 · Permisos nuevos (amplían `rbac`)
| Permiso | Nivel | Roles |
|---|---|---|
| `alerts.view` | contenedor | todos (también `metrics_only`) |
| `alerts.silence` (silenciar, ack, dejar de vigilar) | stack | `admin`, `server_operator`, `app_operator` |
| `alerts.manage` (reglas, comprobaciones, canales, `allow_private_targets`) | organización | `admin` |

- Las claves de API pueden `alerts.view` y `alerts.silence` (útil para silenciar desde un despliegue), **nunca
  `alerts.manage`** (secretos de canales). Las métricas usan los permisos que ya existen (`server.view`,
  `container.view`).

### D9 · Panel
- **Gráficos propios en SVG** (sin librería nueva): componente `TimeSeriesChart` (líneas, eje de tiempo, tooltip,
  claro/oscuro, rango 1 h/6 h/24 h/7 d/30 d). En la pestaña Resumen del servidor (CPU, RAM, disco, red) y una línea
  pequeña de CPU/RAM por contenedor en la pantalla del stack.
- Pantalla **Alertas** (entrada en la navegación con contador, como la maqueta 1): activas, historial y silencios;
  acciones ack, silenciar (duración + motivo), dejar de vigilar. Flota: borde de color y KPI «Alertas activas».
- Ajustes → **Alertas**: reglas, comprobaciones (con último resultado y latencia) y canales (alta, prueba, activar).

## Pasos

Cada paso sube el parche (`bash scripts/bump-version.sh 0.5.N`) con su entrada en los dos CHANGELOG (volver a poner
la sección *Sin publicar* tras el script); el cierre sube a 0.6.0. Ejecutor por defecto: AgentRelay (codex no puede
ejecutar tests en su sandbox: pedirle las pruebas y ejecutarlas el orquestador). Marca ✅ al terminar.

| # | Paso | Archivos | Terminado cuando | Ejecutor · esfuerzo | Versión |
|---|---|---|---|---|---|
| 1 ✅ | Contrato y agente (D2): `Container.health`, eventos de Docker con rebote | `proto/agent.proto` (+ generado), `agent/internal/inventory/**`, `agent/internal/link/link.go` | pruebas: parseo de salud; un evento falso provoca un inventario en < 3 s; corte del flujo → reintento | AgentRelay · medio | 0.5.2 |
| 2 ✅ | Migración 0014 y store de métricas (D1): extensión, hypertables, `COPY` por lotes, downsampling y retención | `core/internal/store/migrations/0014_metrics.sql`, `core/internal/store/metrics.go`, `core/internal/metrics/**`, `core/internal/config/config.go` (`[metrics]`) | pruebas con Postgres: buckets idempotentes, `drop_chunks` respeta la retención, ninguna función TSL (grep en la prueba) | AgentRelay · alto | 0.5.3 |
| 3 ✅ | Hub → `Recorder` (tasas de red en el core) y API `GET /servers/{id}/metrics`, `GET /servers/{id}/containers/{name}/metrics` en `routePerms` | `core/internal/hub/hub.go` (solo el gancho), `core/internal/api/metrics.go`, `core/internal/api/api.go` | pruebas: resolución por rango, contador que baja, permisos (stack-only ve solo sus contenedores) | AgentRelay · medio | 0.5.4 |
| 4 ✅ | Panel: `TimeSeriesChart` y gráficos en Resumen y en el stack (D9) | `web/src/components/chart/**`, `web/src/screens/server/OverviewTab.tsx`, `web/src/screens/server/StackScreen.tsx`, `web/src/api/metrics.ts`, locales | capturas claro/oscuro/móvil; pruebas | AgentRelay · medio | 0.5.5 |
| 5 ✅ | Permisos (D8) y migración 0015 de alertas: reglas (con las de defecto), `alert`, `alert_watch`, `silence`, `check`, `check_result`, `notification_channel`, `notification_delivery`; store | `core/internal/rbac/**`, `core/internal/store/migrations/0015_alerts.sql`, `core/internal/store/alerts.go` | pruebas rbac (claves sin `alerts.manage`) y de store (índice único de huella abierta) | AgentRelay · alto | 0.5.6 |
| 6 ✅ | `netguard` (D6) | `core/internal/netguard/**` | pruebas de tabla de rangos (también IPv4 mapeada), DNS que resuelve a privada, redirección a privada, metadatos bloqueados con `allow_private_targets` | AgentRelay · alto + **revisión Opus** | 0.5.7 |
| 7 ✅ | Motor de alertas (D3, D4): evaluación, estados, histéresis, enfriamiento, silencios, vigilados, gracia, silencio por reinicio; ganchos en stop/down/remove/reboot | `core/internal/alerts/**`, ganchos de una línea en `core/internal/api/{containers,stacks,system}.go` | pruebas con reloj falso: **contenedor vigilado parado → firing en < 30 s; disco 91 % → firing en la siguiente muestra**; sin duplicados; parada desde Talaia no avisa | AgentRelay · alto + **revisión Opus** | 0.5.8 |
| 8 ✅ | Comprobaciones (D5) con `netguard` | `core/internal/checks/**` | pruebas con `httptest` (TLS con certificado que caduca, timeout, códigos, redirecciones) | AgentRelay · medio | 0.5.9 |
| 9 ✅ | Notificadores y cola (D7), mensajes i18n | `core/internal/notify/**`, `core/i18n/*` | pruebas con servidores falsos por tipo, firma HMAC, reintentos, redacción del token en errores, SMTP falso que exige STARTTLS | AgentRelay · alto | 0.5.10 |
| 10 ✅ | API de alertas, silencios, reglas, comprobaciones, canales y prueba de envío; auditoría; arranque de motor, comprobaciones y cola en `cmd` | `core/internal/api/{alerts,alert_rules,checks,channels}.go`, `core/internal/api/api.go`, `core/cmd/**` | pruebas: secretos nunca en respuestas, permisos por rol, guardiana `chi.Walk` en verde | AgentRelay · medio | 0.5.11 |
| 11 ✅ | Panel: pantalla Alertas, contador en la navegación, borde y KPI en Flota | `web/src/screens/Alerts*.tsx`, `web/src/screens/alerts/**`, `web/src/screens/fleet/**`, `web/src/api/alerts.ts`, locales | capturas; pruebas por rol (viewer no ve botones de silenciar) | AgentRelay · medio | 0.5.12 |
| 12 ✅ | Panel: Ajustes → reglas, comprobaciones y canales | `web/src/screens/settings/{AlertRules,Checks,Channels}*.tsx`, `web/src/api/alertsAdmin.ts`, locales | capturas; formulario de canal sin mostrar secretos | AgentRelay · medio | 0.5.13 |
| 13 | Docs EN+ES (métricas, alertas, canales, SSRF), **revisión de seguridad Opus**, prueba real del criterio con el mantenedor (parar un contenedor y llenar un disco en el Debian de pruebas) y cierre 0.6.0 | `docs/**`, ROADMAP, TODO | aviso recibido en < 1 min en los dos casos | Opus + mantenedor | 0.6.0 |

Paralelismo (archivos disjuntos): 1, 2 y 5 a la vez; 3 tras 2; 4 tras 3; 6 en cualquier momento; 7 tras 1, 3 y 5;
8 tras 5 y 6; 9 tras 5 y 6; 10 tras 7-9; 11 y 12 tras 10. No editar el repo mientras corre una ejecución.

Fuera de la fase (a *Ideas* si sale): comprobaciones desde el agente, varios discos por servidor, alertas por
paquetes de seguridad, `talaiactl alerts`, página de estado pública, PWA con push.

## Cómo seguir

`/clear` → `/model sonnet` → «ejecuta los pasos 1, 2 y 5 de `.agents/plans/fase5/fase5.PLAN.md`» (orquestar con
AgentRelay y revisar). Los pasos 6 y 7 los revisa Opus antes de aceptarlos. Ante un imprevisto de diseño, el
ejecutor para y lo anota aquí.
