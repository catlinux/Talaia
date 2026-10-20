# Bloque de operación: actualizaciones desde el panel y acceso externo

Estado: **diseño de Opus (2026-10-20), sin implementar.** Prioridad del mantenedor: poder actualizar y gestionar desde fuera de casa. Se ejecuta antes de cerrar la fase 2 (decisión del mantenedor; fuera del ROADMAP original).

## Principio que manda

Talaia se instala desde GitHub en máquinas que no conocemos. **No puede depender de lo que ya haya en el destino**: ni de un proxy inverso, ni de que los puertos 80/443 estén libres, ni de un servicio de VPN de terceros. Todo lo que necesita lo trae o lo pide en el instalador, con puertos propios que comprueba antes de usarlos.

## Decisiones

1. **Core en un servidor con IP pública** (de momento el VPS de `talaia.invierteconcriterio.es`, instalación desde cero). Los agentes conectan **hacia fuera** al core: un servidor detrás de NAT (el Debian de casa) llega sin VPN.
2. **Headscale se descarta** para este bloque (pendiente de confirmar por el mantenedor): pide 443 para producción (su documentación lo recomienda con fuerza porque los clientes de Tailscale lo asumen en algunos casos), lo que rompe el principio, y con el core público ya no hace falta para lo pedido. Pasa a *Ideas*.
3. **Dos puertos propios**, elegidos por el instalador entre los libres (por defecto 8443 y 8444; nunca 80/443 salvo que estén libres y la persona lo pida):
   - **Panel** (`panel_port`): panel, API y WebSocket de terminal. Certificado válido si hay ACME; si no, el de la CA propia.
   - **Agentes** (`agent_port`): stream gRPC, `/enroll`, `/renew`, `/ca.pem`, `/install.sh`, `/agent/*`. **Siempre** con el certificado de la CA propia.
   - Razón de no usar el certificado público con los agentes: hoy fijan la huella de la CA. Si confiaran en cualquier certificado válido para el nombre, quien controle el DNS (una cuenta de OVH robada) podría hacerse pasar por el core y mandar órdenes como root a toda la flota. Separar puertos mantiene esa garantía y permite exigir certificado de cliente en el puerto de agentes (salvo `/enroll`, `/ca.pem`, `/install.sh`, `/agent/*`).
4. **Certificado válido del panel por ACME DNS-01**, dentro del core con la biblioteca `lego` (`github.com/go-acme/lego/v4`). DNS-01 funciona en cualquier puerto y no necesita 80/443. Proveedor configurable (empezar por `ovh`; importar solo los proveedores elegidos, no el registro completo, y medir el tamaño del binario). Credenciales por las variables propias de lego (`OVH_ENDPOINT` y, por ejemplo, `OVH_APPLICATION_KEY`/`_SECRET`/`OVH_CONSUMER_KEY` con derechos solo `POST`/`DELETE /domain/zone/*`; admiten sufijo `_FILE`), en un `env_file` del compose con modo 0600. Cuenta y certificado en `/var/lib/talaia/acme/`; renovación 30 días antes; cambio en caliente con `tls.Config.GetCertificate`. Sin ACME configurado, todo sigue como hoy (CA propia + instrucciones para confiar en ella).
5. **Actualizaciones con un ayudante del host** (`talaia-hostd`, bash + systemd), porque el core (contenedor de solo lectura, sin capacidades) no debe recibir el socket de Docker:
   - El core comparte con el host un directorio montado (`/var/lib/talaia-host` → `/var/lib/talaia/host` en el contenedor, propiedad del uid 65532). El core **solo** escribe `requests/update.json` (`{"target":"<sha de 40 hex>"}`) y lee `status.json`.
   - Una unidad `talaia-update.path` vigila el fichero y lanza `talaia-update.service` (oneshot, root) → `talaia-hostd update`, que:
     1. valida el JSON (sha de 40 hex; nada más se lee de él), toma un candado, anota el commit actual;
     2. `git fetch origin main` en el repositorio registrado en `/etc/talaia/hostd.conf` y exige que el sha esté en `origin/main` (`git merge-base --is-ancestor`); nunca acepta otra rama ni una ruta;
     3. hace copia de la base de datos (`docker compose exec -T db pg_dump`, guarda las 3 últimas en `/var/lib/talaia-host/backups`, modo 0600);
     4. `git merge --ff-only <sha>` y `TALAIA_GIT_COMMIT=<sha> docker compose up -d --build`;
     5. espera a `https://127.0.0.1:<panel_port>/healthz` (hasta 3 min);
     6. si falla: vuelve al commit anterior, restaura la copia, reconstruye y marca `rolled_back`;
     7. escribe `status.json` (`state`, `from`, `to`, `started_at`, `finished_at`, últimas líneas del registro) en cada fase.
   - El repositorio en el host lo clona el instalador con su propio usuario; el ayudante ejecuta git como ese usuario (`runuser -u`), no como root, para no tocar permisos ni disparar hooks como root.
6. **Detección en el core** (`core/internal/updates`): cada 30 min (configurable) pregunta a la API pública de GitHub por `main` del repositorio configurado (`update_repo`, por defecto `catlinux/Talaia`), con peticiones condicionales (`ETag`); calcula commits pendientes con `compare/<actual>...<último>` y solo ofrece el último commit cuyo CI terminó en verde (`actions/runs?head_sha=`). El commit propio se graba al construir (`ARG GIT_COMMIT` → `-X main.commit`). Sin commit conocido: «versión desconocida» y se ofrece el último en verde.
7. **Mudanza futura** (dominio y VPS nuevos): nada fija el nombre. El agente guarda la URL del core en `/etc/talaia/agent.toml`; hará falta una orden tipada para cambiarla antes de mudar (paso 9, más adelante).

## Pasos

Cada paso: un commit (o pocos), pruebas y entrada en *Unreleased* de `CHANGELOG.md` y `CHANGELOG.es.md`. **No se sube versión** (regla del proyecto: solo cuando el mantenedor lo pida). El ejecutor marca ✅ aquí y, ante un imprevisto de diseño, para y lo anota.

| # | Paso | Archivos | Hecho cuando | Ejecutor |
|---|---|---|---|---|
| 1 | Dos escuchas en el core (panel y agentes) con los ajustes `panel_listen`/`agent_listen` y `panel_port`/`agent_port` públicos (sustituyen a `public_port`; la orden de instalación de agentes usa el de agentes, los orígenes el del panel). El puerto de agentes exige certificado de cliente en el stream y `/renew`. Compose publica los dos | `core/internal/config`, `core/cmd/talaia-core`, `core/internal/api`, `packaging/core/compose.yaml`, `.env.example` | pruebas de config y de enrutado (ninguna ruta de agentes en el panel ni al revés); CI `core stack (docker)` adaptado y en verde | Sonnet, alto |
| 2 | Instalador del core: elige puertos libres (`ss -ltn`), nunca 80/443 por defecto, pregunta el nombre público, escribe `.env`, y ofrece ACME (proveedor y credenciales a un `env_file` 0600) | `packaging/core/install.sh` | prueba en el CI con 8443 ocupado: elige otro y arranca | Sonnet, alto |
| 3 | Certificado ACME DNS-01 del panel con lego (proveedor `ovh`; estructura para añadir más), renovación y recarga en caliente; sin ACME, CA propia | `core/internal/acme` (nuevo), `core/cmd/talaia-core` | pruebas contra un servidor ACME de pruebas (pebble o el *staging* de Let's Encrypt fuera del CI) y de la recarga; tamaño del binario anotado | Sonnet, alto |
| 4 | Detección de actualizaciones y API: `GET /api/v1/system/version`, `POST /api/v1/system/update` (`confirm`, sesión completa, auditoría `system_update_requested`), escritura del fichero de petición y lectura de `status.json` | `core/internal/updates` (nuevo), `core/internal/api`, Dockerfile (`GIT_COMMIT`) | pruebas con un servidor de GitHub simulado (ETag, CI rojo/verde, sin commit propio) | Sonnet, medio |
| 5 | Ayudante del host: `talaia-hostd`, unidades `.path`/`.service`, `hostd.conf`, instalación desde `install.sh` | `packaging/core/hostd/` (nuevo), `packaging/core/install.sh` | prueba en el CI: petición válida actualiza, petición con otra rama o JSON roto se rechaza, un core que no arranca vuelve atrás | **Opus**, alto (código root delicado) |
| 6 | Pantalla de **Ajustes** en el panel: tarjeta de versión (actual, pendientes, mensajes de los commits, CI), botón con confirmación, progreso que sobrevive al reinicio del core; ca/es/en; capturas claro/oscuro/móvil | `web/src/screens/Settings*`, `web/src/api/system.ts`, locales | tests de vitest y capturas | Sonnet, medio |
| 7 | Documentación EN/ES: instalar en un VPS con otros servicios, crear la credencial de OVH con derechos mínimos, actualizar desde el panel, qué hace el ayudante; sustituir la sección 6 de la guía | `docs/en/installation.md`, `docs/es/instalacion.md`, `README*` | revisada contra lo construido | Sonnet, bajo |
| 8 | Revisión de seguridad de 1–6 | — | hallazgos corregidos | **Opus**, alto |
| 9 | Instalación real en `talaia.invierteconcriterio.es` con el mantenedor (necesita push aprobado, `sudo` en el VPS y la credencial de OVH); el Debian de casa como servidor; prueba de una actualización de punta a punta | — | panel con candado desde fuera de casa, agente del Debian conectado, una actualización aplicada desde el panel | sesión con el mantenedor |
| 10 | Más adelante: orden tipada para cambiar la URL del core en los agentes (mudanza) y actualización de agentes desde el core | — | — | por diseñar |

## Cómo retomarlo

`/clear` → `/model sonnet` → «ejecuta el paso 1 de `.agents/plans/operacion/operacion.PLAN.md`». El paso 5 y el 8 se hacen con `/model opus`.
