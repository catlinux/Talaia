# Fase 6 · Despliegues, dominios y plantillas (0.7.0)

Estado: **plan escrito (2026-10-28, Opus); M1-M6 cerradas el 2026-10-30.** Pasos 1 a 4 hechos (0.6.4); el 3 y el 4 revisados.
Criterio del ROADMAP: *«hacer push a `main` publica la app por HTTPS sin hacer nada más»*. En modo externo (M1) se
cumple a partir del segundo despliegue: el bloque de Apache de cada dominio se pone una vez, a mano, y desde ahí cada
push publica solo.

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

## Decisiones del mantenedor (cerradas el 2026-10-30; M1, M2 y M4 delegadas en Opus)

Requisito del mantenedor que manda sobre todo lo demás: **el VPS real tiene muchos servicios (Apache en 80/443 con
otras webs) y ninguno puede verse afectado; todo debe convivir.** Ver *Convivencia* más abajo.

| # | Decisión | Pasos |
|---|---|---|
| M1 | **Modo externo por defecto; Caddy gestionado solo a petición.** Si 80/443 están ocupados (el VPS real), Talaia no instala ningún proxy: la app escucha en `127.0.0.1:<puerto libre>` y el panel da el bloque de Apache (y nginx) listo para pegar, con los pasos de certbot. **Talaia nunca escribe, recarga ni reinicia Apache ni otro servidor web.** El modo gestionado (Caddy en 80/443) solo se ofrece si los dos puertos están libres y el usuario lo elige a mano en ese servidor. El modo gestionado se prueba con e2e en local, no en el VPS real. | 7, 13 |
| M2 | **Solo Dockerfile en esta fase.** El repo de la app debe traer `Dockerfile` (ruta configurable). El constructor automático (Nixpacks/Railpack) pasa a *Ideas*: más dependencia de terceros y builds más pesados en un servidor compartido. El antiguo paso 6 se elimina. | 5 |
| M3 | **Plantillas = instalación en un clic de apps conocidas** (elegir, poner dominio y poco más; Talaia genera el stack con contraseñas aleatorias). Lista: Uptime Kuma, Vaultwarden, Forgejo, Nextcloud, WordPress, Ghost, Umami, Plausible CE, Paperless-ngx, Miniflux, FreshRSS, Jellyfin, Immich, Homepage y Mealie (más PostgreSQL, MariaDB y Redis como bloques). El mantenedor puede cambiar nombres hasta el paso 10. Revisar la licencia de cada imagen (solo se referencian). Sin logotipos. | 10 |
| M4 | **Solo GitHub** (el mantenedor solo usa GitHub): webhook firmado `X-Hub-Signature-256` y **sondeo** de la rama cada 2 min como alternativa. Solo URLs de `github.com` (HTTPS o SSH) con la clave de host de GitHub fijada; sin TOFU. GitLab y Gitea/Forgejo → *Ideas* (añadirlos después es solo otra comprobación de firma y otra clave de host). | 3, 4 |
| M5 | **Entornos por pull request fuera** (ya están en *After 1.0* del ROADMAP). | — |
| M6 | **La prueba real es en el VPS de producción.** Reglas en *Convivencia*: nada se instala en el VPS hasta el final de la fase; la prueba usa un subdominio y un repo de prueba y el mantenedor ejecuta él mismo, paso a paso, lo que toque Apache. | 15 |

## Convivencia con otros servicios y reglas para el VPS real (M1, M6)

- Talaia solo toca lo que ha creado como app: stacks con la etiqueta `dev.talaia.app`, imágenes `talaia/<stack>:*`,
  redes `talaia-*` y `/var/lib/talaia/builds/`. **Nunca** `docker system prune`, ni `image prune` general, ni stacks
  o contenedores ajenos, ni Apache.
- Puertos: el agente elige el puerto de la app en un rango propio (por defecto 20000-29999), comprobando sockets en
  escucha y puertos publicados por Docker; siempre en `127.0.0.1`, nunca en `0.0.0.0`. Un puerto ocupado = error
  claro, nunca se quita a otro servicio.
- Recursos: límites de CPU/RAM por defecto en cada app (editables) y un solo build a la vez por servidor; el build
  se niega con menos de 2 GiB libres.
- El panel avisa antes de crear un dominio que ya sirve otra web (resolver y comparar), sin bloquear.
- Desarrollo y pruebas siempre en local (Postgres portable, Docker local/e2e). **No se actualiza el VPS durante la
  fase**: solo al final, por el botón del panel y con CI en verde. La migración `0017` solo crea tablas nuevas.
- Prueba real (paso 15): el mantenedor crea un subdominio de prueba, Talaia despliega un repo de ejemplo en
  `127.0.0.1`, y el mantenedor añade el bloque de Apache siguiendo instrucciones paso a paso (`apachectl configtest`
  antes de `systemctl reload apache2`). Después: push a `main` → la app cambia sola; volver atrás; borrar la app deja
  el servidor como estaba.

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
  `known_hosts`: solo las claves publicadas de `github.com`, fijas en el agente (M4); cualquier otro host se
  rechaza. La columna `app.known_host` se mantiene para cuando haya más proveedores.
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
- Firma: solo GitHub `X-Hub-Signature-256` (HMAC-SHA256, comparación en tiempo constante; M4). Secreto por app,
  sellado, mostrado **una vez**. El evento `ping` de GitHub responde 200 sin desplegar.
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
- Modo **externo** (M1, **por defecto**): sin Caddy; la app se publica en `127.0.0.1:<puerto>` y el panel muestra
  el bloque para Apache y nginx (con `ProxyPass`, cabeceras `X-Forwarded-*` y WebSocket) y los pasos de certbot.
  El agente no toca nunca la configuración del servidor web.
- El modo gestionado solo se puede activar si el agente comprueba que 80 y 443 están libres en ese momento.
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
| 1 ✅ | Proto: `AppSource`, `AppBuild`, `AppRelease`, `ProxyApply`, `ProxyInstall`, `GitProbe`; campos de proxy (modo, 80/443 libres) en `Inventory` | `proto/agent.proto` (+ generado) | `buf lint` y `buf breaking` en verde; ningún número reusado | Sonnet (sesión) · medio | 0.6.1 |
| 2 ✅ | Migración 0017 y store de apps, variables, despliegues y dominios; permiso `server.proxy` | `core/internal/store/migrations/0017_apps.sql`, `core/internal/store/apps.go`, `core/internal/rbac/**` | pruebas con BD: únicos, secretos sellados, `superseded`; rbac (`app_operator` no `stack.edit`); la migración solo crea tablas | AgentRelay · alto | 0.6.2 |
| 3 ✅ | Agente: Git endurecido (D2), solo `github.com` | `agent/internal/source/**` | pruebas: esquemas, hosts y argumentos rechazados, hooks ignorados, submódulos no, clave de host fijada, temporales borrados | AgentRelay · alto + **revisión Opus** | 0.6.3 |
| 4 ✅ | Core: webhook firmado de GitHub y sondeo (D4) | `core/internal/api/hooks.go`, `core/internal/deploy/poll.go`, `core/internal/api/authz.go` | pruebas: firma mala/ausente/repetida, `ping`, otra rama, cuerpo grande, límite; guardiana `chi.Walk` en verde | AgentRelay · alto + **revisión Opus** | 0.6.4 |
| 5 | Agente: build con Dockerfile (D3), uno a la vez por servidor | `agent/internal/build/**` | pruebas con runner falso: argumentos exactos, espacio libre, tiempo, secretos fuera de build-args, limpieza solo de lo suyo | AgentRelay · medio | 0.6.5 |
| 6 | Agente: modo externo por defecto (puerto libre en `127.0.0.1`) y Caddy gestionado opcional (D5) | `agent/internal/proxy/**` | pruebas: elección de puerto sin pisar sockets ni puertos Docker; 80/443 ocupados → gestionado imposible; JSON de Caddy, socket Unix, digest; nunca toca otro servidor web | AgentRelay · alto + **revisión Opus** | 0.6.6 |
| 7 | Agente: release sin corte, reversión y limpieza de imágenes (D3) | `agent/internal/release/**` | pruebas: nuevo sano → viejo fuera; nuevo enfermo → viejo sigue y `failed`; se guardan 3 imágenes; solo borra `talaia/<stack>:*` | AgentRelay · alto + **revisión Opus** | 0.6.7 |
| 8 | Core: canalización de despliegue (cola por app, estados, log, generación del compose con límites de CPU/RAM, «editada a mano») | `core/internal/deploy/**` | pruebas con agente falso: push → build → release → `succeeded`; push durante otro → `superseded`; fallo de build no toca el stack | AgentRelay · alto | 0.6.8 |
| 9 | API de apps, variables, dominios, despliegues, reversión y proxy (incluido el bloque de Apache/nginx); auditoría | `core/internal/api/{apps,domains,deployments,proxy}.go`, `core/internal/api/api.go`, `core/internal/api/authz.go` | pruebas por rol; secretos nunca en respuestas; clave de API puede desplegar y no editar | AgentRelay · medio | 0.6.9 |
| 10 | Plantillas (D7) según M3 | `templates/**`, `core/internal/templates/**` | prueba que valida todas con `docker compose config` (en CI) y los parámetros | AgentRelay · medio | 0.6.10 |
| 11 | Panel: Apps (lista y ficha con despliegues, log en vivo, volver atrás, variables) | `web/src/screens/Apps*.tsx`, `web/src/screens/apps/**`, `web/src/api/apps.ts`, locales | capturas claro/oscuro/móvil; pruebas por rol | AgentRelay · alto | 0.6.11 |
| 12 | Panel: asistente de nueva app (maqueta 4) | `web/src/screens/apps/wizard/**`, locales | capturas; los cinco pasos con los tres orígenes; en modo externo, el paso Dominio muestra el bloque de Apache | AgentRelay · alto | 0.6.12 |
| 13 | Panel: Dominios (estado del certificado o «lo gestiona tu Apache») y Plantillas; modo de proxy en el servidor | `web/src/screens/{Domains,Templates}*.tsx`, `web/src/screens/server/**` | capturas; pruebas | AgentRelay · medio | 0.6.13 |
| 14 | Docs EN+ES (apps, GitHub y webhooks, modo externo con Apache paso a paso, Caddy, plantillas, seguridad), e2e del modo gestionado en local y **revisión de seguridad Opus** de toda la fase | `docs/**`, `test/e2e/**` | guía paso a paso del criterio comprobada en local | Opus | 0.6.14 |
| 15 | Prueba real en el VPS con el mantenedor (M6, *Convivencia*): actualizar por el panel, app de prueba desde GitHub, bloque de Apache a mano, push a `main`, volver atrás, borrar; cierre | ROADMAP, README, TODO | push → la app cambia por HTTPS sin tocar nada; los demás servicios del VPS siguen igual | Opus + mantenedor | 0.7.0 |

Paralelismo posible (archivos disjuntos), siempre una ejecución de AgentRelay a la vez: 1 primero; luego 2, 3, 5 y 6
en cualquier orden; 4 tras 2; 7 tras 5 y 6; 8 tras 2, 3, 5 y 7; 9 tras 8; 10 tras 9; 11-13 tras 9.

Fuera de la fase (a *Ideas*): entornos por pull request, build sin Dockerfile (Nixpacks/Railpack), GitLab y
Gitea/Forgejo, configurar Apache automáticamente, dominios comodín y DNS-01 para apps, registro de imágenes,
`talaiactl deploy`, regla de alerta `deploy_failed`, builds en otro servidor, varios servicios con despliegue sin
corte.

## Cómo seguir

1. `/clear` → `/model sonnet` → «ejecuta el paso 1 de `.agents/plans/fase6/fase6.PLAN.md`» y seguir en orden.
   Los pasos 3, 4, 6 y 7 los revisa Opus antes de aceptarlos (`/model opus` → «revisa el paso N de fase6»).
2. Ante un imprevisto de diseño, el ejecutor para y lo anota aquí.
3. Nada se instala en el VPS real hasta el paso 15.
