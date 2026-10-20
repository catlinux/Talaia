# Bloque de operaciÃ³n: actualizaciones desde el panel y acceso externo

Estado: **diseÃ±o de Opus (2026-10-20), sin implementar.** Prioridad del mantenedor: poder actualizar y gestionar desde fuera de casa. Se ejecuta antes de cerrar la fase 2 (decisiÃ³n del mantenedor; fuera del ROADMAP original).

## Principio que manda

Talaia se instala desde GitHub en mÃ¡quinas que no conocemos. **No puede depender de lo que ya haya en el destino**: ni de un proxy inverso, ni de que los puertos 80/443 estÃ©n libres, ni de un servicio de VPN de terceros. Todo lo que necesita lo trae o lo pide en el instalador, con puertos propios que comprueba antes de usarlos.

## Decisiones

1. **Core en un servidor con IP pÃºblica** (de momento el VPS de `talaia.invierteconcriterio.es`, instalaciÃ³n desde cero). Los agentes conectan **hacia fuera** al core: un servidor detrÃ¡s de NAT (el Debian de casa) llega sin VPN.
2. **Headscale se descarta** para este bloque (confirmado por el mantenedor el 2026-10-20; la VPN queda en *Ideas* del TODO como opciÃ³n futura): pide 443 para producciÃ³n (su documentaciÃ³n lo recomienda con fuerza porque los clientes de Tailscale lo asumen en algunos casos), lo que rompe el principio, y con el core pÃºblico ya no hace falta para lo pedido. Pasa a *Ideas*.
3. **Dos puertos propios**, elegidos por el instalador entre los libres (por defecto 8443 y 8444; nunca 80/443 salvo que estÃ©n libres y la persona lo pida):
   - **Panel** (`panel_port`): panel, API y WebSocket de terminal. Certificado vÃ¡lido si hay ACME; si no, el de la CA propia.
   - **Agentes** (`agent_port`): stream gRPC, `/enroll`, `/renew`, `/ca.pem`, `/install.sh`, `/agent/*`. **Siempre** con el certificado de la CA propia.
   - RazÃ³n de no usar el certificado pÃºblico con los agentes: hoy fijan la huella de la CA. Si confiaran en cualquier certificado vÃ¡lido para el nombre, quien controle el DNS (una cuenta de OVH robada) podrÃ­a hacerse pasar por el core y mandar Ã³rdenes como root a toda la flota. Separar puertos mantiene esa garantÃ­a y permite exigir certificado de cliente en el puerto de agentes (salvo `/enroll`, `/ca.pem`, `/install.sh`, `/agent/*`).
4. **Certificado vÃ¡lido del panel por ACME DNS-01**, dentro del core con la biblioteca `lego` (`github.com/go-acme/lego/v4`). DNS-01 funciona en cualquier puerto y no necesita 80/443. Proveedor configurable (empezar por `ovh`; importar solo los proveedores elegidos, no el registro completo, y medir el tamaÃ±o del binario). Credenciales por las variables propias de lego (`OVH_ENDPOINT` y, por ejemplo, `OVH_APPLICATION_KEY`/`_SECRET`/`OVH_CONSUMER_KEY` con derechos solo `POST`/`DELETE /domain/zone/*`; admiten sufijo `_FILE`), en un `env_file` del compose con modo 0600. Cuenta y certificado en `/var/lib/talaia/acme/`; renovaciÃ³n 30 dÃ­as antes; cambio en caliente con `tls.Config.GetCertificate`. Sin ACME configurado, todo sigue como hoy (CA propia + instrucciones para confiar en ella).
5. **Actualizaciones con un ayudante del host** (`talaia-hostd`, bash + systemd), porque el core (contenedor de solo lectura, sin capacidades) no debe recibir el socket de Docker:
   - El core comparte con el host un directorio montado (`/var/lib/talaia-host` â†’ `/var/lib/talaia/host` en el contenedor, propiedad del uid 65532). El core **solo** escribe `requests/update.json` (`{"target":"<sha de 40 hex>"}`) y lee `status.json`.
   - Una unidad `talaia-update.path` vigila el fichero y lanza `talaia-update.service` (oneshot, root) â†’ `talaia-hostd update`, que:
     1. valida el JSON (sha de 40 hex; nada mÃ¡s se lee de Ã©l), toma un candado, anota el commit actual;
     2. `git fetch origin main` en el repositorio registrado en `/etc/talaia/hostd.conf` y exige que el sha estÃ© en `origin/main` (`git merge-base --is-ancestor`); nunca acepta otra rama ni una ruta;
     3. hace copia de la base de datos (`docker compose exec -T db pg_dump`, guarda las 3 Ãºltimas en `/var/lib/talaia-host/backups`, modo 0600);
     4. `git merge --ff-only <sha>` y `TALAIA_GIT_COMMIT=<sha> docker compose up -d --build`;
     5. espera a `https://127.0.0.1:<panel_port>/healthz` (hasta 3 min);
     6. si falla: vuelve al commit anterior, restaura la copia, reconstruye y marca `rolled_back`;
     7. escribe `status.json` (`state`, `from`, `to`, `started_at`, `finished_at`, Ãºltimas lÃ­neas del registro) en cada fase.
   - El repositorio en el host lo clona el instalador con su propio usuario; el ayudante ejecuta git como ese usuario (`runuser -u`), no como root, para no tocar permisos ni disparar hooks como root.
6. **DetecciÃ³n en el core** (`core/internal/updates`): cada 30 min (configurable) pregunta a la API pÃºblica de GitHub por `main` del repositorio configurado (`update_repo`, por defecto `catlinux/Talaia`), con peticiones condicionales (`ETag`); calcula commits pendientes con `compare/<actual>...<Ãºltimo>` y solo ofrece el Ãºltimo commit cuyo CI terminÃ³ en verde (`actions/runs?head_sha=`). El commit propio se graba al construir (`ARG GIT_COMMIT` â†’ `-X main.commit`). Sin commit conocido: Â«versiÃ³n desconocidaÂ» y se ofrece el Ãºltimo en verde.
7. **Mudanza futura** (dominio y VPS nuevos): nada fija el nombre. El agente guarda la URL del core en `/etc/talaia/agent.toml`; harÃ¡ falta una orden tipada para cambiarla antes de mudar (paso 9, mÃ¡s adelante).

## Pasos

Cada paso: un commit (o pocos), pruebas y entrada en *Unreleased* de `CHANGELOG.md` y `CHANGELOG.es.md`. **No se sube versiÃ³n** (regla del proyecto: solo cuando el mantenedor lo pida). El ejecutor marca âœ… aquÃ­ y, ante un imprevisto de diseÃ±o, para y lo anota.

| # | Paso | Archivos | Hecho cuando | Ejecutor |
|---|---|---|---|---|
| 1 âœ… | Dos escuchas en el core (panel y agentes) con los ajustes `panel_listen`/`agent_listen` y `panel_port`/`agent_port` pÃºblicos (sustituyen a `public_port`; la orden de instalaciÃ³n de agentes usa el de agentes, los orÃ­genes el del panel). El puerto de agentes exige certificado de cliente en el stream y `/renew`. Compose publica los dos | `core/internal/config`, `core/cmd/talaia-core`, `core/internal/api`, `packaging/core/compose.yaml`, `.env.example` | pruebas de config y de enrutado (ninguna ruta de agentes en el panel ni al revÃ©s); CI `core stack (docker)` adaptado y en verde | Sonnet, alto |
| 2 âœ… | Instalador del core: elige puertos libres (`ss -ltn`), nunca 80/443 por defecto, pregunta el nombre pÃºblico, escribe `.env`, y ofrece ACME (proveedor y credenciales a un `env_file` 0600) | `packaging/core/install.sh` | prueba en el CI con 8443 ocupado: elige otro y arranca | Sonnet, alto |
| 3 âœ… | Certificado ACME DNS-01 del panel con lego (proveedor `ovh`; estructura para aÃ±adir mÃ¡s), renovaciÃ³n y recarga en caliente; sin ACME, CA propia | `core/internal/acme` (nuevo), `core/cmd/talaia-core` | pruebas contra un servidor ACME de pruebas (pebble o el *staging* de Let's Encrypt fuera del CI) y de la recarga; tamaÃ±o del binario anotado (15,6 â†’ 17,4 MB con `-ldflags='-s -w'`) | Sonnet, alto |
| 4 ✅ | DetecciÃ³n de actualizaciones y API: `GET /api/v1/system/version`, `POST /api/v1/system/update` (`confirm`, sesiÃ³n completa, auditorÃ­a `system_update_requested`), escritura del fichero de peticiÃ³n y lectura de `status.json` | `core/internal/updates` (nuevo), `core/internal/api`, Dockerfile (`GIT_COMMIT`) | pruebas con un servidor de GitHub simulado (ETag, CI rojo/verde, sin commit propio) | Sonnet, medio |
| 5 | Ayudante del host: `talaia-hostd`, unidades `.path`/`.service`, `hostd.conf`, instalaciÃ³n desde `install.sh` | `packaging/core/hostd/` (nuevo), `packaging/core/install.sh` | prueba en el CI: peticiÃ³n vÃ¡lida actualiza, peticiÃ³n con otra rama o JSON roto se rechaza, un core que no arranca vuelve atrÃ¡s | **Opus**, alto (cÃ³digo root delicado) |
| 6 | Pantalla de **Ajustes** en el panel: tarjeta de versiÃ³n (actual, pendientes, mensajes de los commits, CI), botÃ³n con confirmaciÃ³n, progreso que sobrevive al reinicio del core; ca/es/en; capturas claro/oscuro/mÃ³vil | `web/src/screens/Settings*`, `web/src/api/system.ts`, locales | tests de vitest y capturas | Sonnet, medio |
| 7 | DocumentaciÃ³n EN/ES: instalar en un VPS con otros servicios, crear la credencial de OVH con derechos mÃ­nimos, actualizar desde el panel, quÃ© hace el ayudante; sustituir la secciÃ³n 6 de la guÃ­a | `docs/en/installation.md`, `docs/es/instalacion.md`, `README*` | revisada contra lo construido | Sonnet, bajo |
| 8 | RevisiÃ³n de seguridad de 1â€“6 | â€” | hallazgos corregidos | **Opus**, alto |
| 9 | InstalaciÃ³n real en `talaia.invierteconcriterio.es` con el mantenedor (necesita push aprobado, `sudo` en el VPS y la credencial de OVH); el Debian de casa como servidor; prueba de una actualizaciÃ³n de punta a punta | â€” | panel con candado desde fuera de casa, agente del Debian conectado, una actualizaciÃ³n aplicada desde el panel | sesiÃ³n con el mantenedor |
| 10 | MÃ¡s adelante: orden tipada para cambiar la URL del core en los agentes (mudanza) y actualizaciÃ³n de agentes desde el core | â€” | â€” | por diseÃ±ar |

## CÃ³mo retomarlo

`/clear` â†’ `/model sonnet` â†’ Â«ejecuta el paso 1 de `.agents/plans/operacion/operacion.PLAN.md`Â». El paso 5 y el 8 se hacen con `/model opus`.
