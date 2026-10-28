# Fase 6 · Despliegues, dominios y plantillas (0.7.0)

Estado: **plan escrito (2026-10-28, Opus). Bloqueado hasta que el mantenedor cierre M1-M6.** Ningún paso empezado.
Criterio del ROADMAP: *«hacer push a `main` publica la app por HTTPS sin hacer nada más»*.

Punto de partida (0.6.0): un **stack** es un proyecto Compose en `/opt/talaia/stacks/<nombre>/` (`compose.yaml` +
`.env`), o uno adoptado que vive en su propio directorio. Comandos tipados `StackWrite` (valida con
`docker compose config` antes de instalar), `StackAction` (`up -d --remove-orphans`, `down`, `pull`, `restart`,
`remove`, `forget`), `StackRead` y `StackAdopt` (`agent/internal/stacks/stacks.go`, ejecuta `docker` con argumentos
fijos, nunca una shell). `JobEvent.output` lleva texto acotado por el agente; tabla `job` (`0005_jobs.sql`) con
`jobTimeout(kind)` en `core/internal/api/containers.go`. Permisos de stack: `stack.view`, `stack.deploy` (desplegar
lo que ya existe), `stack.edit` (cambiar archivos: **equivale a root**). Secretos con `SecretBox`
(`core/internal/auth/secretbox.go`). Salida del core protegida por `netguard`. Última migración: `0016`. No hay
tablas de stacks: viven en disco y en el inventario.

Principio de la maqueta 4: **una app es un stack Compose normal** que Talaia genera y que se puede editar a mano
después. Nada de formato propio oculto: si Talaia desaparece, el stack sigue funcionando con `docker compose`.

Reglas de toda la fase (CLAUDE.md §3 y las de la fase 5): permisos en el core; i18n ca/es/en; docs EN+ES; estilo
según la memoria `feedback-estilo-visual` (capturas claro/oscuro/móvil); proto sin renumerar; **sin dependencias Go
o npm nuevas salvo que el paso lo diga**; toda imagen de terceros que Talaia arranque (Caddy, builder) se fija por
**digest** y se lista en `CREDITS.md`/`THIRD-PARTY-NOTICES.txt` (recordar al mantenedor el script de licencias).

## Decisiones del mantenedor (cerrar antes de empezar)

| # | Pregunta | Recomendación | Bloquea |
|---|---|---|---|
| M1 | **Caddy y los puertos 80/443.** El proxy gestionado necesita 80 y 443 en el servidor de la app (Let's Encrypt HTTP-01/TLS-ALPN). El VPS del mantenedor ya tiene Apache ahí. | **Modo doble por servidor:** *gestionado* (Talaia instala Caddy en 80/443, solo si están libres; nunca toca otro servidor web) o *externo* (Talaia publica la app en `127.0.0.1:<puerto>` y muestra el bloque de configuración para Apache/nginx; el HTTPS lo pone el proxy existente). El criterio de cierre se prueba en modo gestionado en un servidor con 80/443 libres. | 7, 8, 13 |
| M2 | **Builds sin Dockerfile.** Por lo que sé (hasta mediados de 2026), Railway dejó Nixpacks en mantenimiento y lo sustituye por Railpack. **Verificarlo antes de decidir.** | Fase 6 con **Dockerfile** obligatorio (paso 5) y el constructor automático como paso separado (paso 6) con el que esté vivo; si no hay uno mantenido y con licencia compatible, el paso 6 pasa a *Ideas*. | 6 |
| M3 | **Las 15 plantillas.** | Propuesta: Uptime Kuma, Vaultwarden, Forgejo, Nextcloud, WordPress, Ghost, Umami, Plausible CE, Paperless-ngx, Miniflux, FreshRSS, Jellyfin, Immich, Homepage y Mealie (más PostgreSQL, MariaDB y Redis como bloques). Revisar la licencia de cada imagen (solo se referencian, no se redistribuyen). Sin logotipos (marcas). | 11 |
| M4 | **Proveedores Git.** | GitHub, GitLab y Gitea/Forgejo (los tres firman o llevan token). Y **sondeo** de la rama cada 2 min como alternativa, para servidores que no reciben webhooks. Verificar que GitHub acepta webhooks a un puerto no estándar (`:8443`); si no, el sondeo cubre ese caso. | 4 |
| M5 | **Entornos por pull request** (la maqueta los muestra). | Fuera de la fase → *Ideas*. | — |
| M6 | **Entorno de la prueba real.** | Un servidor con 80/443 libres (el Debian de casa o un VPS de pruebas), un subdominio que apunte a él y un repositorio de ejemplo con Dockerfile. | 16 |

## Decisiones técnicas (tomadas; cambiar solo con motivo)

### D1 · Modelo: la app envuelve un stack
- Tablas (migración `0017_apps.sql`):
  - `app(id, server_id, stack_name, source git|image|template, repo_url, branch, build dockerfile|auto,
    dockerfile_path, build_context, image, service, port, auto_deploy, zero_downtime, template_id,
    template_version, deploy_key_pub, deploy_key_enc, webhook_secret_enc, known_host, created_at, updated_at,
    updated_by)`; único `(server_id, stack_name)`.
  - `app_env(app_id, key, value_enc, secret)`: todas las variables selladas; las `secret` nunca vuelven a la API.
  - `deployment(id, app_id, number, trigger push|poll|manual|rollback|create, commit_sha, commit_subject,
    image_ref, state queued|building|releasing|succeeded|failed|superseded, started_at, finished_at, error,
    log)`; `log` acotado a 256 KiB (lo que no cabe, el final).
  - `domain(id, app_id, hostname, service, port, created_at)`; `hostname` único global; validado como nombre DNS
    (sin comodines en esta fase).
- El core genera el `compose.yaml` del stack (servicio `app` con la imagen `talaia/<stack>:<sha12>`, red
  `talaia-proxy` externa, `restart: unless-stopped`, límites de CPU/RAM del asistente, etiquetas
  `dev.talaia.app=<id>`) y lo envía con `StackWrite`. Si el usuario edita el stack a mano, la app pasa a
  **«editada a mano»**: Talaia solo cambia la etiqueta de imagen del servicio `app` y avisa si ese servicio ya no
  existe. Nunca reescribe lo que el usuario cambió.

### D2 · Git en el agente, endurecido
- Comando tipado `AppSource` (fetch) en el agente: `git` con argumentos fijos, nunca shell. Solo `https://` y SSH
  (`ssh://` o `usuario@host:ruta`); se rechaza todo lo demás (`file:`, `ext::`, rutas locales, cualquier argumento
  que empiece por `-`). Siempre `-c protocol.allow=never -c protocol.https.allow=always -c
  protocol.ssh.allow=always -c core.hooksPath=/dev/null -c submodule.recurse=false`, `GIT_TERMINAL_PROMPT=0`,
  `GIT_CONFIG_NOSYSTEM=1`, `HOME` vacío y temporal. Clon superficial de **un commit concreto** de la rama.
- Repos privados: **clave de despliegue ed25519 por app** generada en el core (la pública se muestra para pegarla en
  el proveedor; la privada sellada). Viaja en el comando, se escribe 0600 en un temporal y se borra al acabar.
  `known_hosts`: fijos los de GitHub, GitLab y Codeberg; para otros, **TOFU** guardado en `app.known_host` y
  visible en el panel; un cambio de clave falla el despliegue.
- Espacio de trabajo `/var/lib/talaia/builds/<stack>/` 0700, borrado tras cada build; límite de tamaño del clon
  (1 GiB) y tiempo (5 min).

### D3 · Build y despliegue
- Build en el **servidor de la app** (sin registro): `docker build` con argumentos fijos, `--pull`, sin
  `--privileged`, sin red del host, etiqueta `talaia/<stack>:<sha12>`, tiempo máximo 30 min, y se niega si quedan
  menos de 2 GiB libres. Las variables `secret` **no** son build-args (solo las marcadas «en build»).
- **Sin corte** (solo apps de un servicio sin volúmenes; se desactiva sola en las demás): escalar `app` a 2 con
  `--no-recreate`, esperar a que el nuevo esté sano (healthcheck del Dockerfile o, si no hay, conexión TCP al
  puerto), quitar el viejo y volver a 1. Si el nuevo no arranca sano en 120 s se elimina y **el viejo sigue
  sirviendo**: despliegue `failed`, sin corte. Sin la opción: `up -d` normal.
- **Volver atrás**: se conservan las 3 últimas imágenes correctas; revertir = desplegar la imagen de un
  `deployment` anterior sin build. La limpieza borra las demás imágenes `talaia/<stack>:*`.
- Cola por app en el core (`core/internal/deploy`): un despliegue a la vez; un push nuevo deja como `superseded`
  el que esperaba (no el que está construyendo). Estado y log en `deployment`, con el progreso del agente.

### D4 · Webhooks y sondeo
- Ruta pública `POST /api/v1/hooks/git/{app_id}` (declarada en `routePerms` como pública con firma; la prueba
  guardiana de `chi.Walk` debe seguir verde). Cuerpo máx. 1 MiB, límite por IP y por app.
- Firma: GitHub `X-Hub-Signature-256` (HMAC-SHA256), Gitea/Forgejo `X-Gitea-Signature`, GitLab
  `X-Gitlab-Token` (comparación en tiempo constante). Secreto por app, sellado, mostrado **una vez**.
- Del cuerpo solo se usa la rama y el commit; solo eventos push a la rama configurada; deduplicado por id de
  entrega. El agente comprueba que ese commit es el que tiene la rama al hacer el fetch (si no, usa el de la rama
  y lo anota).
- Sondeo opcional por app (`ls-remote` desde el agente, cada 2 min, con el mismo endurecimiento) para servidores
  sin webhook.

### D5 · Proxy (Caddy) por servidor
- Modo **gestionado**: el agente mantiene un stack interno `talaia-proxy` con Caddy fijado por digest, puertos
  80/443 y la red Docker `talaia-proxy`. La API de administración de Caddy escucha en un **socket Unix** de un
  volumen que solo lee el agente, nunca en TCP. El core manda la configuración completa con un comando tipado
  (`ProxyApply`: lista de `{hostname, upstream servicio:puerto}`); el agente la convierte a JSON de Caddy (no
  acepta JSON del core) y la carga. Certificados automáticos de Caddy (HTTP-01/TLS-ALPN).
- Al añadir un dominio, el core avisa (sin bloquear) si no resuelve a la IP del servidor.
- Modo **externo** (M1): sin Caddy; la app se publica en `127.0.0.1:<puerto>` y el panel muestra el bloque para
  Apache y nginx.
- El inventario informa del modo, de la versión de Caddy y del estado de cada certificado (para la pantalla
  Dominios).

### D6 · Permisos
- Una app es un stack: ver = `stack.view`; desplegar, revertir y ver logs de build = `stack.deploy`; crear, cambiar
  origen, build, variables y dominios, borrar = `stack.edit` (root, como hoy). Las claves de API pueden
  `stack.deploy` (despliegues desde CI).
- Nuevo `server.proxy` (nivel servidor; `admin` y `server_operator`): instalar o quitar Caddy y elegir el modo.
- Las variables `secret`, la clave privada y el secreto del webhook **nunca** salen por la API ni en logs.
- Todo se audita: crear/borrar app, cambios de origen/variables/dominios, cada despliegue y reversión, proxy.

### D7 · Plantillas
- En el repo: `templates/<id>/template.yaml` (nombre, descripción i18n, compose con parámetros `{{domain}}`,
  `{{password:32}}`, versión de imagen fijada, puerto, requisitos de RAM) y su prueba que valida todas. Se
  publican con el core (no se descargan de internet en tiempo de ejecución). Crear desde plantilla = app
  `source=template` sin build.

### D8 · Panel
- Navegación (maqueta 4): **Apps**, **Dominios**, **Plantillas**.
- Asistente de 5 pasos (Origen · Construcción · Destino · Dominio · Revisión) desde Git, plantilla o imagen, con
  resumen lateral; muestra la clave pública y la URL + secreto del webhook al final.
- Ficha de app: estado, despliegues (log en vivo, reintentar, volver atrás), variables, dominios, ajustes.

## Pasos

Cada paso sube el parche (`bash scripts/bump-version.sh 0.6.N`; poner antes la entrada en *Sin publicar* de los dos
CHANGELOG, o el script falla) y marca ✅ aquí y en los dos TODO. Ejecutor por defecto: AgentRelay, uno a la vez
(codex no puede ejecutar los tests: pedirlos y ejecutarlos el orquestador). **No editar el repo mientras corre.**

| # | Paso | Archivos | Terminado cuando | Ejecutor · esfuerzo | Versión |
|---|---|---|---|---|---|
| 1 | Proto: `AppSource`, `AppBuild`, `AppRelease`, `ProxyApply`, `ProxyInstall`, `GitProbe`; campos de proxy en `Inventory` | `proto/agent.proto` (+ generado) | `buf lint` y `buf breaking` en verde; ningún número reusado | Sonnet (sesión) · medio | 0.6.1 |
| 2 | Migración 0017 y store de apps, variables, despliegues y dominios; permiso `server.proxy` | `core/internal/store/migrations/0017_apps.sql`, `core/internal/store/apps.go`, `core/internal/rbac/**` | pruebas con BD: únicos, secretos sellados, `superseded`; rbac (`app_operator` no `stack.edit`) | AgentRelay · alto | 0.6.2 |
| 3 | Agente: Git endurecido (D2) | `agent/internal/source/**` | pruebas: esquemas y argumentos rechazados, hooks ignorados, submódulos no, TOFU, cambio de clave falla, temporales borrados | AgentRelay · alto + **revisión Opus** | 0.6.3 |
| 4 | Core: webhooks firmados de los tres proveedores y sondeo (D4) | `core/internal/api/hooks.go`, `core/internal/deploy/poll.go`, `core/internal/api/authz.go` | pruebas: firma mala/ausente/repetida, otra rama, cuerpo grande, límite; guardiana `chi.Walk` en verde | AgentRelay · alto + **revisión Opus** | 0.6.4 |
| 5 | Agente: build con Dockerfile (D3) | `agent/internal/build/**` | pruebas con runner falso: argumentos exactos, espacio libre, tiempo, secretos fuera de build-args, limpieza | AgentRelay · medio | 0.6.5 |
| 6 | Agente: build automático sin Dockerfile (según M2) | `agent/internal/build/auto*.go` | prueba con un proyecto Node y uno Go de ejemplo | AgentRelay · medio | 0.6.6 |
| 7 | Agente: proxy Caddy gestionado y modo externo (D5) | `agent/internal/proxy/**` | pruebas: JSON generado, socket Unix, imagen por digest, 80/443 ocupados → modo externo con aviso | AgentRelay · alto + **revisión Opus** | 0.6.7 |
| 8 | Agente: release sin corte, reversión y limpieza de imágenes (D3) | `agent/internal/release/**` | pruebas: nuevo sano → viejo fuera; nuevo enfermo → viejo sigue y `failed`; se guardan 3 imágenes | AgentRelay · alto + **revisión Opus** | 0.6.8 |
| 9 | Core: canalización de despliegue (cola por app, estados, log, generación del compose, «editada a mano») | `core/internal/deploy/**` | pruebas con agente falso: push → build → release → `succeeded`; push durante otro → `superseded`; fallo de build no toca el stack | AgentRelay · alto | 0.6.9 |
| 10 | API de apps, variables, dominios, despliegues, reversión y proxy; auditoría | `core/internal/api/{apps,domains,deployments,proxy}.go`, `core/internal/api/api.go`, `core/internal/api/authz.go` | pruebas por rol; secretos nunca en respuestas; clave de API puede desplegar y no editar | AgentRelay · medio | 0.6.10 |
| 11 | Plantillas (D7) según M3 | `templates/**`, `core/internal/templates/**` | prueba que valida todas con `docker compose config` (en CI) y los parámetros | AgentRelay · medio | 0.6.11 |
| 12 | Panel: Apps (lista y ficha con despliegues, log en vivo, volver atrás, variables) | `web/src/screens/Apps*.tsx`, `web/src/screens/apps/**`, `web/src/api/apps.ts`, locales | capturas claro/oscuro/móvil; pruebas por rol | AgentRelay · alto | 0.6.12 |
| 13 | Panel: asistente de nueva app (maqueta 4) | `web/src/screens/apps/wizard/**`, locales | capturas; los cinco pasos con los tres orígenes | AgentRelay · alto | 0.6.13 |
| 14 | Panel: Dominios (estado del certificado) y Plantillas; modo de proxy en el servidor | `web/src/screens/{Domains,Templates}*.tsx`, `web/src/screens/server/**` | capturas; pruebas | AgentRelay · medio | 0.6.14 |
| 15 | Docs EN+ES (apps, Git y webhooks, proxy, plantillas, seguridad) y **revisión de seguridad Opus** de toda la fase | `docs/**` | guía paso a paso del criterio comprobada | Opus | 0.6.15 |
| 16 | Prueba real con el mantenedor (M6): crear la app desde Git, push a `main`, ver la app por HTTPS; volver atrás; cierre | ROADMAP, README, TODO | push → HTTPS sin tocar nada más | Opus + mantenedor | 0.7.0 |

Paralelismo posible (archivos disjuntos), siempre una ejecución de AgentRelay a la vez: 1 primero; luego 2, 3, 5 y 7
en cualquier orden; 4 tras 2; 8 tras 5 y 7; 9 tras 2, 3, 5 y 8; 10 tras 9; 11 tras 10; 12-14 tras 10.

Fuera de la fase (a *Ideas*): entornos por pull request, dominios comodín y DNS-01 para apps, registro de imágenes,
`talaiactl deploy`, regla de alerta `deploy_failed`, builds en otro servidor, varios servicios con despliegue sin
corte.

## Cómo seguir

1. El mantenedor contesta M1-M6 (se anotan aquí, en *Decisiones del mantenedor*).
2. `/clear` → `/model sonnet` → «ejecuta el paso 1 de `.agents/plans/fase6/fase6.PLAN.md`» y seguir en orden.
   Los pasos 3, 4, 7 y 8 los revisa Opus antes de aceptarlos (`/model opus` → «revisa el paso N de fase6»).
3. Ante un imprevisto de diseño, el ejecutor para y lo anota aquí.
