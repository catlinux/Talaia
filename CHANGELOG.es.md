# Registro de cambios

[Read in English](CHANGELOG.md)

Todos los cambios relevantes del proyecto se documentan en este archivo.

El formato se basa en [Keep a Changelog](https://keepachangelog.com/es-ES/1.1.0/) y el proyecto sigue el
[Versionado Semántico](https://semver.org/lang/es/). La versión actual está en el archivo
[`VERSION`](VERSION).

Secciones de cada versión: **Añadido**, **Cambiado**, **Obsoleto**, **Eliminado**, **Corregido**,
**Seguridad**.

## [Sin publicar]

## [0.3.22] - 2026-10-22

### Corregido

- La barra de filtros del Journal queda alineada: los campos con texto de ayuda debajo ya no desplazan sus entradas más abajo que las demás.
- **Reload** solo se ofrece a los servicios que lo admiten (`CanReload` de systemd, que el agente informa como `can_reload`), en lugar de fallar con «Job type reload is not applicable».
- La insignia de la pestaña **Actualizaciones** sigue la última lectura del estado (tras *Comprobar ahora* o una actualización) en lugar de las cifras cargadas al abrir la pantalla.
- El registro en vivo de una actualización de apt muestra las líneas según las escribe apt-get (salida por líneas) en lugar de quedarse vacío durante minutos.

## [0.3.21] - 2026-10-22

### Corregido

- Una instalación nueva del agente ya no se queda sin conectar: el instalador escribía `core_url` y `ca_fingerprint` al final de `/etc/talaia/agent.toml` y, tras la nueva tabla `[terminal]`, quedaban escondidos dentro de ella. Ahora van al principio, el `agent.toml` del paquete vuelve a su contenido anterior y el agente avisa claramente si esas claves están dentro de `[terminal]`. Lo detectó la prueba de instalación del CI.
- Instalar o actualizar el paquete del agente (desde el panel o con la orden de instalación) conserva el `/etc/talaia/agent.toml` editado sin preguntar. Antes, un cambio de ese archivo en un paquete nuevo habría parado dpkg con una pregunta que nadie puede contestar y el agente habría quedado a medio instalar.
- El resultado de un trabajo llega al core aunque el canal con el core se haya perdido y reconectado mientras corría (una actualización de apt puede tardar). Antes, el core lo daba por en curso hasta su tiempo límite.
- Las acciones de servicio funcionan con instancias como `getty@tty1.service`: se rechazaba la `@` que el panel escapa en la dirección.

### Seguridad

- Un reinicio ya no puede empezar en mitad de una actualización de apt. Si una actualización supera el tiempo límite de su trabajo, o un proceso anterior del agente dejó una en marcha, el agente mantiene el bloqueo de apt y sigue la unidad hasta que `apt-get` termina; hasta entonces un reinicio u otra actualización se rechazan con el mensaje *another apt operation is running*. Encontrado en la revisión de seguridad de la fase 3.
- Un código TOTP erróneo al abrir una terminal del host queda ahora en la auditoría como `host_terminal_denied` (nunca el código). Encontrado en la misma revisión, que también cubrió la terminal del host, las acciones de servicio, la búsqueda en el journal y la auditoría sin otros hallazgos.

## [0.3.20] - 2026-10-22

### Añadido

- Guía de la pantalla de servidor entero (`docs/es/servidor.md`): actualizaciones y reinicio, servicios, registro del sistema, terminal del host y auditoría, con límites y solución de problemas. Los documentos de seguridad, arquitectura e instalación describen lo que la fase 3 puede hacer como root, qué se registra (nunca el código TOTP, el texto buscado, las líneas del registro ni las listas de paquetes) y cómo desactivar la terminal del host con `[terminal] host = false`.

## [0.3.19] - 2026-10-22

### Añadido

- La pantalla de servidor se organiza ahora en pestañas: **Resumen** (medidores de CPU, memoria y disco, una lista *Necesita atención* que salta a la pestaña adecuada —reinicio necesario, actualizaciones de seguridad, disco lleno, servicios fallidos, actualización del agente— y los datos de la máquina), **Contenedores**, **Servicios**, **Registros** y **Actualizaciones**, con insignias de recuento. Pantalla nueva de **Auditoría** con todas las acciones registradas, de la más nueva a la más antigua, filtros por grupo de acciones y usuario, detalles y *Cargar más*. En ca, es y en.

## [0.3.18] - 2026-10-22

### Añadido

- El core sirve el registro de auditoría, solo lectura: `GET /api/v1/audit` devuelve primero los eventos más nuevos con un cursor (`next`/`before`) y filtros por acción (exacta o por prefijo, como `service_*`), servidor y usuario. Leerlo no se audita. Todavía sin acceso desde el panel.

## [0.3.17] - 2026-10-22

### Añadido

- Terminal en el propio servidor desde el panel: un botón **Terminal** en la pantalla de servidor abre un shell de root tras una confirmación y un código TOTP nuevo de 6 cifras, con una franja roja permanente *Root en <servidor>* y la marca de grabación; sale desactivado con una explicación si el agente la tiene apagada o es demasiado viejo. En ca, es y en.

## [0.3.16] - 2026-10-22

### Añadido

- El core abre una terminal de root grabada en el propio servidor (`POST /api/v1/servers/{id}/terminal`): exige `confirm`, un código TOTP nuevo (verificado con la misma protección contra reutilización que el acceso y el cambio de contraseña, así que un código no se puede reutilizar) y un agente con la terminal del host activada. Se graba como las terminales de contenedor, se audita como `host_terminal_open` y `host_terminal_close`, una por usuario y servidor, y sale como `host` en `talaia-core terminal list`. Todavía sin acceso desde el panel.

## [0.3.15] - 2026-10-22

### Añadido

- El agente puede abrir un shell de root en el propio servidor (todavía sin acceso desde el panel): un shell de login de una lista cerrada (`bash` o `sh`) en un pseudo-terminal local con entorno limpio, los mismos límites que las terminales de contenedor más un máximo de 2 sesiones de host, y se mata todo el grupo de procesos al acabar la sesión. Se puede desactivar con `[terminal] host = false` en `agent.toml` (o `TALAIA_AGENT_TERMINAL_HOST`) y el agente lo informa en su inventario. Dependencia nueva: `github.com/creack/pty` (MIT).

## [0.3.14] - 2026-10-22

### Añadido

- Registro del sistema en el panel: la pantalla de servidor tiene una sección **Registro del sistema** con filtros (servicio, prioridad, arranque, rango de fechas, búsqueda de texto), un modo **Seguir** que trae las líneas nuevas cada pocos segundos, resaltado del texto buscado y un botón de descarga. En ca, es y en.

## [0.3.13] - 2026-10-22

### Añadido

- El core lee el registro del sistema de un servidor (`POST /api/v1/servers/{id}/journal`) con filtros validados: servicio, prioridad, rango de fechas, arranque, búsqueda de texto y un cursor para seguir. La auditoría anota que se leyó, nunca el texto buscado ni las líneas. Todavía sin acceso desde el panel.

## [0.3.12] - 2026-10-22

### Añadido

- El agente lee el registro del sistema (journald) con filtros (servicio, prioridad, rango de fechas, arranque, búsqueda de texto literal) y un cursor para seguir las líneas nuevas (todavía sin acceso desde el panel). El texto de búsqueda siempre se escapa y nada pasa por un shell.

## [0.3.11] - 2026-10-22

### Añadido

- Servicios en el panel: la pantalla de servidor tiene una sección **Servicios** con búsqueda, filtros (activos, fallidos, inactivos), el estado y si arrancan al inicio, y las acciones iniciar, parar, reiniciar, recargar, activar y desactivar (parar, reiniciar y desactivar preguntan antes, con un aviso más fuerte para servicios que pueden cortarte el acceso como SSH, Docker o la red). El servicio del agente no tiene acciones de parada. En ca, es y en.

## [0.3.10] - 2026-10-22

### Añadido

- El core lista los servicios de systemd de un servidor (`GET /api/v1/servers/{id}/services`) y ejecuta `start`, `stop`, `restart`, `reload`, `enable` o `disable` sobre uno (`POST …/services/{unit}/actions`; parar, reiniciar y desactivar exigen `confirm`; solo servicios que el agente informó; el servicio del agente no se puede parar desde el panel). Auditado como `service_<acción>`. Todavía sin acceso desde el panel.

## [0.3.9] - 2026-10-22

### Añadido

- El agente informa de si cada servicio está activado y puede iniciar, parar, reiniciar, recargar, activar o desactivar un servicio (todavía sin acceso desde el panel). Se niega a pararse, reiniciarse o desactivarse a sí mismo.

## [0.3.8] - 2026-10-22

### Añadido

- Actualizar y reiniciar desde el panel: la sección **Actualizaciones** tiene los botones **Actualizar seguridad** y **Actualizar todo** (con confirmación previa; nunca se eliminan paquetes, se conserva la configuración local y avisa si Docker se reiniciaría) con el registro de apt en vivo mientras corre, y una acción **Reiniciar** que pide escribir el nombre de host del servidor. En ca, es y en.

## [0.3.7] - 2026-10-22

### Añadido

- El core puede actualizar paquetes y reiniciar un servidor: `POST /api/v1/servers/{id}/apt/upgrade` (`mode`: `security` o `all`, exige `confirm: true`) y `POST /api/v1/servers/{id}/reboot` (la confirmación debe ser el nombre de host que informa el agente, así no puede llegar a otra máquina). Solo una actualización o reinicio por servidor a la vez. Auditados como `apt_upgrade` y `host_reboot`. Todavía sin acceso desde el panel.

## [0.3.6] - 2026-10-22

### Añadido

- El agente puede actualizar paquetes y reiniciar (todavía sin acceso desde el panel): `AptUpgrade` ejecuta `apt-get` en una unidad transitoria de systemd con un registro, de modo que una actualización sobrevive a un reinicio del agente y este informa del resultado al volver; los modos son *solo seguridad* (paquetes que calcula el propio agente) y *todo* (nunca elimina paquetes, conserva la configuración local); una sola operación de apt a la vez. `HostReboot` comprueba el nombre del servidor y reinicia cinco segundos después de informar del éxito.

## [0.3.5] - 2026-10-22

### Añadido

- Actualizaciones en el panel (solo lectura): la pantalla de servidor tiene una sección **Actualizaciones** con los paquetes pendientes (primero los de seguridad), los retenidos, el motivo del reinicio, cuándo se actualizaron las listas y un botón **Comprobar ahora** que ejecuta `apt-get update`; las tarjetas de Flota muestran una insignia de actualizaciones (roja si hay de seguridad) y otra de **Reinicio necesario**. En ca, es y en.

## [0.3.4] - 2026-10-22

### Añadido

- El core guarda el resumen de apt y la opción de terminal del host de cada agente conectado y los muestra en `GET /api/v1/servers` (`live.apt`, `live.host_terminal`); `POST /api/v1/servers/{id}/apt/status` pide al agente la lista completa de actualizaciones pendientes, paquetes retenidos y motivos de reinicio (con `refresh` ejecuta antes `apt-get update`). Todavía no se ve en el panel.

## [0.3.3] - 2026-10-22

### Añadido

- El agente lee apt sin cambiar nada: simula una actualización al arrancar y cada 6 horas, cuenta las actualizaciones pendientes y de seguridad, los paquetes retenidos y si hace falta reiniciar (archivo marcador o un kernel instalado más nuevo), manda el resumen en su inventario y responde a la orden `AptStatus` con la lista completa (pudiendo ejecutar antes `apt-get update`). Todavía no se ve en el panel.

## [0.3.2] - 2026-10-22

### Añadido

- Contrato del agente para la fase 3 (servidor entero): órdenes tipadas para servicios de systemd, journald, estado y actualización de apt, y reinicio; un resumen de apt y la opción de terminal del host en el inventario; el estado de activación de cada servicio; un destino para las sesiones de terminal (contenedor o host). Todavía no los usa nada.

## [0.3.1] - 2026-10-22

### Cambiado

- **Licencia cambiada a WNCL-TP-1.0.** Cubre solo el material propio de Talaia y deja claro que los componentes de terceros conservan su licencia. Las condiciones de Talaia no cambian: uso, estudio, modificación y distribución gratis con fines no comerciales; venderlo o el uso comercial requieren permiso por escrito. La pantalla Acerca de y el paquete del agente muestran el nuevo identificador.

### Añadido

- `CREDITS.md` y `CREDITS.es.md` listan cada componente de terceros de los binarios y del panel web (40 módulos Go, 40 paquetes npm y 3 fuentes OFL) con versión, licencia y enlace.
- `THIRD-PARTY-NOTICES.txt` con el texto de la licencia y el aviso de copyright de cada componente, generado por `scripts/third-party-notices.mjs`. Se instala con el paquete del agente (`/usr/share/doc/talaia-agent/`) y va en la imagen del core (`/usr/share/doc/talaia/`), junto con `LICENSE`, y el CI falla si está desfasado.

## [0.3.0] - 2026-10-22

### Cambiado

- **Fase 2 (contenedores y stacks) cerrada.** Contenedores, stacks con el editor de Compose, adopción de proyectos en marcha, imágenes/volúmenes/redes con limpieza, registros en directo, la pantalla de stack y la terminal de contenedor grabada se probaron en dos servidores reales; la revisión de seguridad de la fase está hecha. El roadmap marca las fases 0 a 2 como hechas y la fase 3 (servidor completo) es la siguiente.

### Seguridad

- Entrada con PIN: el intento se cuenta ahora antes de comprobar el PIN, en una sola sentencia junto con su límite, así que peticiones enviadas a la vez no pueden probar más de cinco PIN por dispositivo de confianza (antes, peticiones en paralelo desde varias direcciones podían comprobarse todas antes de que el contador desactivara el dispositivo). Encontrado en la revisión de seguridad que cierra la fase 2, que cubrió también las passkeys, la actualización de agentes y la terminal de contenedor sin otros hallazgos.

## [0.2.15] - 2026-10-22

### Corregido

- Un stack adoptado cuya carpeta o archivo compose desaparece del servidor ya no se queda como una entrada muerta: el agente lo informa (`broken_stacks`, campo 16 del proto), `GET /api/v1/servers/{id}/stacks` lo lista en `broken`, y el panel lo marca **Faltan los archivos** y ofrece solo **Dejar de gestionar** (las demás acciones responden que los archivos ya no están). Los agentes antiguos siguen funcionando sin la marca.

## [0.2.14] - 2026-10-22

### Cambiado

- Guía de acceso: aclara que «navegador cerrado» significa que el navegador ha descartado sus cookies de sesión, así que Chrome en el ordenador con *Continuar donde lo dejaste* o aplicaciones en segundo plano mantiene el PIN sin segundo factor hasta que pasan `device_mfa_hours`, y cómo cambiarlo.

## [0.2.13] - 2026-10-22

### Corregido

- Terminal de contenedor: en algunos navegadores la terminal crecía sin fin y luego dejaba de aceptar teclas, con miles de errores «WebSocket is already in CLOSING or CLOSED state» en la consola. La caja de la terminal tiene ahora una altura propia (ya no sigue a su contenido), el redimensionado solo se envía cuando cambian de verdad las filas o columnas y nunca por una conexión cerrada, y el tamaño deja de vigilarse cuando la sesión termina.

## [0.2.12] - 2026-10-22

### Cambiado

- Parar un contenedor desde la pantalla del servidor pide ahora confirmación antes, igual que eliminarlo (iniciar y reiniciar siguen siendo inmediatos), porque parar una base de datos o un servidor web por error deja servicios caídos.

## [0.2.11] - 2026-10-22

### Cambiado

- Adoptar un proyecto Compose cuya carpeta o archivo pueden escribir otros ahora falla indicando la orden exacta para arreglarlo (`chmod go-w '<carpeta>' '<archivo>'`) en vez de una frase genérica.

## [0.2.10] - 2026-10-22

### Corregido

- Panel: los diálogos de confirmación (actualizar, quitar un servidor, actualizar un agente) se abrían al final de las páginas largas, porque la animación de entrada de la página la dejaba como referencia de los elementos fijos; ahora se abren centrados en la ventana. En Ajustes, la tarjeta de progreso de la actualización aparece justo debajo de la tarjeta de versión, no al final de la página. Las pruebas del panel esperan hasta cinco segundos a un elemento, así que un equipo cargado ya no las hace fallar al azar.

## [0.2.9] - 2026-10-22

### Corregido

- Los agentes anteriores a la 0.2.8 ignoran la orden de actualización, así que el panel dejaba la actualización «en curso» para siempre. El núcleo los marca ahora como `agent_update_manual`, envía el comando de actualización en `agent_upgrade_command`, y el panel muestra una etiqueta **Actualización manual** con el comando para copiar en lugar de un botón; a partir de la 0.2.8 se actualizan desde el panel. Un trabajo de actualización atascado deja de bloquear nuevos intentos pasados 15 minutos.

## [0.2.8] - 2026-10-21

### Añadido

- Actualizar los agentes desde el panel: las tarjetas y pantallas de servidor muestran la versión del agente y una etiqueta **Actualización disponible**, la pantalla del servidor tiene el botón **Actualizar agente** y **Ajustes > Servidores** tiene **Actualizar todos los agentes**. El núcleo envía una orden tipada nueva (`AgentUpdate`, campo 22 del proto) con la versión y el SHA-256 del paquete que sirve; el agente lo descarga por su canal con TLS mutuo (límite de 64 MiB), rechaza una versión igual o anterior o un hash erróneo, y lo instala con `apt-get` en una unidad de systemd aparte, de modo que el reinicio del agente no la interrumpe. API nueva: `POST /api/v1/servers/{id}/agent/update`, `POST /api/v1/agents/update-all` y el campo `agent_update_available` de `GET /api/v1/servers`. Se audita como `agent_update`. Guía de seguridad actualizada.

## [0.2.7] - 2026-10-21

### Corregido

- Pedir el comando de enrolamiento de un nombre de servidor que aún tenía un token sin usar (hasta 15 minutos) fallaba con «nombre en uso». El token nuevo sustituye ahora al pendiente, así que se puede volver a pedir un comando perdido al momento.

## [0.2.6] - 2026-10-21

### Corregido

- Reinstalar un agente tras quitar su servidor del panel conservaba la identidad revocada (el núcleo contestaba 403 sin parar) e ignoraba el token nuevo. Un token de enrolamiento junto a una identidad guardada hace ahora que el agente se enrole una vez con él, sea cual sea el núcleo que emitió la identidad antigua; si falla, conserva la identidad guardada. Sustituye a la comprobación anterior, que solo comparaba la huella de la CA.

## [0.2.5] - 2026-10-21

### Añadido

- Guía de acceso (`docs/es/acceso.md`, también en inglés y catalán): PIN en un dispositivo de confianza, passkeys, la ventana del segundo factor y cómo deshacer cada método. La guía de instalación explica ahora por qué la dirección del panel lleva puerto (8443 salvo que `--port 443` esté libre), cómo se vuelve a enrolar un agente y cómo quitar un servidor; el README y la guía de seguridad enlazan la guía nueva.

## [0.2.4] - 2026-10-21

### Añadido

- Entrada con PIN o huella en el panel: la pantalla de acceso ofrece primero el PIN de un dispositivo de confianza (con un enlace al formulario de contraseña y el paso de TOTP cuando toca el segundo factor) y un botón **huella o PIN de este dispositivo** para las passkeys. Ajustes tiene una tarjeta **Acceso** nueva para confiar en dispositivos o quitarlos (PIN dos veces y la contraseña) y para añadir o quitar passkeys, con un aviso cuando no están disponibles. En ca, es y en.

## [0.2.3] - 2026-10-21

### Añadido

- Passkeys (WebAuthn, huella o el PIN del dispositivo) en la API del núcleo; las pantallas del panel vienen después. Necesitan un nombre DNS en `public_hostnames` y HTTPS (si no, `passkeys_unavailable`). Una passkey exige siempre verificación del usuario y vale como contraseña más segundo factor. El alta pide sesión completa y la contraseña otra vez (hasta 10 por usuario); `POST /api/v1/auth/passkey/login/begin` y `/finish` inician sesión; `GET` y `DELETE /api/v1/auth/passkeys` las listan y quitan. Los desafíos duran 5 minutos y valen una vez; un contador de firmas que retrocede desactiva la passkey (`passkey_clone_suspected`). Nueva migración `0010`.

## [0.2.2] - 2026-10-21

### Añadido

- Dispositivos de confianza con PIN (API del núcleo; las pantallas del panel vienen después): desde una sesión completa y confirmando de nuevo la contraseña, un navegador se puede registrar como dispositivo de confianza con un PIN de 6 a 12 dígitos (sin dígitos repetidos ni secuencias). `POST /api/v1/auth/pin/login` entra con el PIN y la cookie del dispositivo. El segundo factor se vuelve a pedir al cerrar el navegador o pasadas `device_mfa_hours` (`TALAIA_DEVICE_MFA_HOURS`, 12 por defecto, de 1 a 168). Cinco PIN erróneos desactivan el dispositivo; dispositivo desconocido, PIN erróneo y dispositivo desactivado dan la misma respuesta. Máximo 10 dispositivos por usuario; `GET /api/v1/auth/devices` y `DELETE /api/v1/auth/devices/{id}` los listan y quitan, y cambiar la contraseña los quita todos. Se audita como `device_trusted`, `login_pin`, `login_pin_failed` y `device_removed`. Nueva migración `0009`.

## [0.2.1] - 2026-10-21

### Añadido

- Ajustes: una tarjeta **Servidores** lista los servidores enrolados y permite al administrador quitar uno (`DELETE /api/v1/servers/{id}`, sesión completa y `confirm`; el panel pide escribir el nombre del servidor). La baja es lógica: el servidor sale del panel, se cierra su conexión, se revoca su certificado y su nombre queda libre para enrolarlo de nuevo; las grabaciones de terminal se conservan. Se audita como `server_removed`. Nueva migración `0008`.
- Las actualizaciones dicen qué trae la versión nueva: Ajustes muestra una lista de **Novedades** con las entradas del CHANGELOG añadidas entre la versión en marcha y la ofrecida, en el idioma del panel (el catalán usa el texto en castellano), encima de la lista de commits. El núcleo lee los dos changelogs de `raw.githubusercontent.com` en los dos commits; si falla, la tarjeta muestra solo los commits, como antes.
- El instalador del núcleo y el arranque `install-core.sh` hablan inglés, español y catalán: cada pregunta, error, aviso y el texto de ayuda salen de un catálogo de mensajes. El idioma se toma de `--lang <en|es|ca>` o de `LC_ALL`/`LC_MESSAGES`/`LANG`, con el inglés por defecto; las respuestas siguen siendo `yes`/`no` (en español y catalán se acepta también `sí` y `s`). `test-install.sh` cubre la selección y un error traducido en cada idioma.
- Guía del certificado de confianza con dos registros DNS (`docs/es/dns-delegation.md`, también en inglés y catalán): los registros que hay que crear, con los pasos de OVH, el puerto 53, qué significa cada línea de la comprobación, la seguridad y cómo deshacerlo. La guía de instalación y el README la recomiendan ahora, con la clave de OVH como alternativa.
- Certificado de confianza sin claves del proveedor de DNS: con `acme_provider = delegation` la zona delega `_acme-challenge.<host>` en el servidor del panel con un registro NS, y el nuevo servicio `acme-dns` (`talaia-core dns`, perfil de compose `acme-dns`) contesta el desafío DNS-01 en el puerto 53. Corre aparte del núcleo, de solo lectura, sin base de datos ni secretos, contesta solo ese nombre, nunca hace recursión y limita cada red de origen. `talaia-core acme check-delegation` dice qué registro falta o está mal y si el puerto 53 responde desde internet.
- Guía de OVH para el certificado de confianza (`docs/es/ovh-dns.md`, también en inglés y catalán): cómo crear la clave de la API autorizada a cambiar solo tu zona DNS, con los derechos exactos, qué copiar y qué hacer si algo falla; enlazada desde la guía de instalación.
- Guía de seguridad (`docs/es/seguridad.md`): el administrador es root en cada servidor gestionado (archivos Compose, adopción, terminal de contenedor, actualizaciones), qué protege el panel y los agentes, y cómo se graban, conservan y exportan las sesiones de terminal (`terminal list`, `terminal export`).
- Nuevo arranque `install-core.sh`: el `curl … | bash` de una línea clona el `main` actual en `/opt/talaia` (`TALAIA_DIR`, `TALAIA_REPO_URL`, `TALAIA_BRANCH`), reutiliza un clon existente del mismo repositorio sin hacer pull, pregunta antes de instalar git y pasa todas las opciones del instalador con `bash -s --`; la entrada estándar se reconecta a `/dev/tty` cuando se ejecuta por una tubería. El paso ACME/OVH ahora pregunta la zona y la región, imprime el enlace preparado para la clave y la guía, comprueba la clave con OVH (la firma va en un archivo de configuración modo 0600, nunca en la línea de órdenes) antes de escribir `.env.acme` o construir, nombra los derechos que falten, ofrece volver a introducir las claves o continuar con la CA propia, valida un `.env.acme` existente y añade `--acme-zone` y `--no-acme-check`. README y guías de instalación actualizados; `test-install.sh` cubre la firma, los casos de fallo, la deducción de la zona y el arranque.
- Ajustes: un botón **Comprobar ahora** pregunta a GitHub al momento por commits nuevos mediante `POST /api/v1/system/check` (una comprobación forzada por minuto; `429 rate_limited` muestra un aviso corto) en vez de esperar a la comprobación de cada 30 minutos; el panel sustituye la tarjeta de versión con la respuesta.
- Certificado sin clave de proveedor: el instalador ofrece la delegación DNS como opción recomendada, detecta la IPv4 pública y comprueba el puerto 53, imprime los dos registros DNS y el aviso de AAAA, pide permiso antes de abrir los puertos TCP del panel y de los agentes y UDP/TCP 53 en ufw o firewalld, comprueba con `talaia-core acme check-delegation`, obtiene un certificado de staging, pasa a producción y quita staging si falla; añade `--acme delegation`, `--public-ip` y `--no-acme`. `test-install.sh` cubre las comprobaciones de IP y puerto, las claves de `.env`, los registros, el bucle de comprobación y el cambio de staging.
- Instalación en un VPS desde cero: el instalador comprueba el sistema operativo, git, root o sudo, el espacio en disco y Docker con el plugin Compose antes de escribir nada y dice cómo arreglar lo que falte; `--install-docker` (o un sí en un terminal) instala Docker con el script oficial; avisa si un nombre DNS no resuelve, imprime las órdenes de ufw o firewalld para los dos puertos (nunca los abre), comprueba los extremos del panel y de los agentes, espera hasta tres minutos al certificado ACME, informa del ayudante de actualizaciones y termina con las direcciones, cómo añadir un servidor, cómo actualizar desde Ajustes y dónde están los registros. `test-install.sh` cubre el preflight en el CI.
- Pantalla de stack (maqueta 3): al pulsar el nombre de un stack se ven su ruta de navegación, el número de contenedores en marcha, las acciones (desplegar, reiniciar, parar con confirmación y actualizar imágenes), una tabla con sus contenedores, CPU y memoria, una nota siempre visible en el editor del compose que explica que un archivo Compose se ejecuta como root en el servidor, y los registros del contenedor con un modo **Seguir** que pide cada 3 segundos las líneas posteriores a la última, las deduplica y guarda como máximo 2000.
- Los registros de contenedores se pueden pedir de forma incremental: el trabajo de registros acepta un `since` RFC 3339 opcional y un indicador `timestamps` de extremo a extremo, así que el panel puede pedir solo las líneas nuevas; los trabajos de registros terminados se conservan 15 minutos y después se purgan para que la tabla de trabajos no crezca.
- Ayudante de actualizaciones: instala su propia versión nueva cuando una actualización lo cambia, rechaza peticiones de más de 30 minutos, y el panel avisa de una petición que nadie recoge en cinco minutos como un ayudante parado en vez de esperar sin fin.
- Actualizaciones desde el panel: `install.sh` instala `talaia-hostd`, un ayudante que corre como root y que arranca `talaia-update.path` cuando el núcleo deja una petición en `/var/lib/talaia-host/requests/`. Solo acepta un commit de `origin/main` más nuevo que el que corre, hace copia de la base de datos (guarda las tres últimas en `/var/lib/talaia-hostd/backups`), avanza el clon como su dueño, reconstruye, espera a `/healthz` y, si el núcleo nuevo no responde, vuelve al commit anterior y restaura la copia. El núcleo nunca recibe el socket de Docker; `--no-hostd` omite el ayudante. La imagen registra su commit (`TALAIA_GIT_COMMIT`).
- Guía de instalación: el núcleo en un servidor público sin los puertos 80/443, el certificado de confianza por ACME DNS-01 con OVH (derechos mínimos de la API) y la comprobación de actualizaciones en Ajustes; la VPN queda como alternativa.
- Pantalla de Ajustes en el panel: la versión y el commit en marcha, los commits pendientes de la última compilación en verde con su distintivo de CI, y el botón de actualizar con una confirmación que explica que el panel no estará disponible unos minutos y que antes se hace una copia de la base de datos. La pantalla sigue el progreso del ayudante del host cada tres segundos, sobrevive a un reinicio del core durante la actualización y se reanuda al recargar la página, y muestra el registro cuando una actualización falla o vuelve atrás.
- Detección de actualizaciones y API de petición: `GET /api/v1/system/version` informa de la versión y el commit en marcha, del commit más reciente de `main` con el CI en verde, de los commits pendientes desde la compilación actual y del estado que escribe el ayudante del host; `POST /api/v1/system/update` (sesión completa, confirmación y solo el commit ofrecido) deja `requests/update.json` para el ayudante y se audita como `system_update_requested`. El core pregunta a la API pública de GitHub cada 30 minutos (`update_repo`, `update_check_interval`, `update_api_url`), reutiliza los ETag, espera si se agota el límite de peticiones y sigue funcionando si GitHub no responde. La compilación graba el commit con `-X main.commit` (argumento de compilación `GIT_COMMIT`) y `host_dir` es el directorio compartido con el ayudante.
- El panel puede servir un certificado en el que los navegadores ya confían: cuando se define `acme_provider` / `TALAIA_ACME_PROVIDER` (de momento `ovh`), el core lo obtiene por ACME DNS-01 con lego, sin proxy inverso y sin usar los puertos 80/443, guarda la cuenta y el certificado en `<data_dir>/acme/`, lo renueva 30 días antes de que caduque con reintentos escalonados si falla y lo cambia sin reiniciar, y sigue sirviendo el certificado de la CA propia de Talaia hasta que el público está listo; `acme_email` y `acme_directory_url` (para staging o un servidor de pruebas) son los otros ajustes.
- Contrato de las terminales en contenedores (`TerminalOpen`, `TerminalInput`, `TerminalResize`, `TerminalClose` desde el core; `TerminalOpened`, `TerminalOutput`, `TerminalClosed` desde el agente). Aún sin uso: solo define los mensajes.
- Sesiones de terminal en el agente (aún no accesibles desde el panel): una shell de una lista cerrada (`/bin/sh` o `/bin/bash`) se ejecuta con la API de exec de Docker y un terminal, con el usuario del contenedor y sin privilegios, y termina con el stream hacia el core. Límites: 4 sesiones, 15 minutos sin actividad, 4 horas en total, 16 KiB por mensaje de salida y 1 MiB/s de salida; una shell que no lee su entrada se cierra en lugar de bloquear al agente. Informa de si el contenedor equivale a root del anfitrión (privilegiado, espacio de PID del anfitrión o socket de Docker montado).
- Sesiones de terminal en el core (aún no accesibles desde el panel): las sesiones se enrutan por servidor e id de sesión, así que un agente solo puede alimentar las suyas; como máximo 4 por servidor y una por usuario y contenedor; cada sesión se graba como un archivo asciicast v2 en trozos cifrados con la clave de secretos y atados a su posición, de modo que un trozo quitado, repetido o intercambiado se detecta al exportar; una sesión que no puede grabarse (base de datos caída, 50 MiB alcanzados) se cierra; las pulsaciones no se graban. Las pulsaciones esperan a un agente ocupado en lugar de perderse, y un navegador que lee demasiado despacio cierra su sesión. Se audita como `terminal_open` y `terminal_close`.
- `talaia-core terminal list` y `talaia-core terminal export <id>` (imprime la grabación para `asciinema play`), y el ajuste `terminal_recording_days` / `TALAIA_TERMINAL_RECORDING_DAYS` (por defecto 90, 0 las conserva siempre) con una purga diaria. Las sesiones que un reinicio dejó abiertas se cierran al arrancar.
- API de terminal: `POST /api/v1/servers/{id}/containers/{c}/terminal` (sesión completa; exige `confirm` y una shell `sh` o `bash`) inicia la shell y responde con un ticket de un solo uso, y `GET /api/v1/terminal/{id}/ws` es el WebSocket (los mensajes binarios son pulsaciones y salida, los de texto son de control: ticket, redimensionado, cierre). El ticket dura 30 segundos, se guarda como hash, sirve una vez, y solo para la sesión para la que se emitió desde la sesión del panel que lo pidió; una shell a la que nadie se conecta se cierra. El WebSocket exige un `Origin` permitido (no hay atajo de mismo sitio) y la cookie de sesión, y la sesión del panel se comprueba cada minuto: revocarla cierra el terminal. El core cierra sus sesiones al apagarse.
- Terminal en el panel: un botón **Terminal** en cada contenedor en marcha abre una shell con xterm.js (servido por el propio panel y cargado solo al usarlo). Antes pregunta, explicando que la sesión se graba y que lo que escribes se conserva cuando la pantalla lo muestra, y deja elegir `sh` o `bash`; muestra una marca permanente de *Grabando*, un aviso rojo cuando el contenedor equivale a root del servidor (privilegiado, procesos del anfitrión o socket de Docker) y por qué terminó una sesión (código de salida, inactividad, sesión revocada…) con un botón para abrir otra. En ca/es/en, para tema claro y oscuro y pantallas pequeñas.
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
- Sección de la guía de instalación sobre cómo llegar al panel desde fuera mediante una VPN privada.
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

- La credencial de OVH documentada para ACME queda limitada a las tres llamadas que hace lego sobre una zona, en vez de a todas las zonas de la cuenta. El instalador rechaza nombres de host y correos de ACME con caracteres que romperían `.env`.
- La Content-Security-Policy permite ahora elementos `<style>` de la propia página (`style-src-elem 'self' 'unsafe-inline'`), porque xterm.js escribe así los colores de la terminal y, sin ello, la terminal muestra texto oscuro sobre fondo oscuro. Los scripts, los atributos de estilo en línea, los orígenes de terceros y las URL `data:` siguen bloqueados, y una prueba vigila que no se afloje nada más.
- Revisión de seguridad del enrolamiento: la revocación cierra los streams abiertos, la identidad del agente queda ligada al número de serie del certificado actual o anterior, el enrolamiento es una sola transacción, la clave y el certificado del agente se guardan en un único archivo atómico, `insecure_dev` solo escucha en loopback, los errores permanentes del agente detienen los reinicios de systemd (salida 78) y los directorios de la CA y de los certificados se fuerzan a modo 0700.
- Autenticación del panel (adelantada de la fase 3): administrador único creado desde la consola, contraseña con argon2id, TOTP obligatorio con códigos de recuperación de un solo uso, cookie de sesión segura, protección CSRF, límites contra fuerza bruta, registro de auditoría y pantallas de acceso en ca/es/en.
- Despliegue del core con Docker Compose (`packaging/core/`): una imagen con el core y el panel web, PostgreSQL 16 + TimescaleDB, sistema de archivos de solo lectura y sin capacidades.
- `GET /api/v1/servers` (requiere sesión completa) y la pantalla de flota alimentada por él, refrescada cada 15 segundos; los datos que aún no existen se muestran como no disponibles en lugar de números de ejemplo.
- Revisión de seguridad de los trabajos y los stacks: el agente escribe los archivos de un stack mediante un archivo temporal exclusivo, así que un enlace simbólico dejado en el directorio de un proyecto adoptado ya no puede hacerle sobrescribir otro archivo como root; leer un stack adoptado rechaza los enlaces simbólicos; la adopción rechaza un archivo compose que sea un enlace y archivos o directorios que puedan escribir otros usuarios; un stack nuevo ya no puede tomar el nombre de un proyecto en marcha que Talaia no gestiona (su `up --remove-orphans` borraría los contenedores de ese proyecto).

### Cambiado

- `packaging/core/install.sh` elige los puertos por su cuenta: el primero libre desde 8443 (panel) y 8444 (agentes), comprobado con `ss`, nunca el 80 ni el 443 salvo que se pida; un puerto dado con `--port` o `--agent-port` que esté ocupado detiene el script. Pregunta el nombre público en un terminal y, con `--acme ovh` o si se pide, guarda las credenciales de OVH en `.env.acme` (modo 0600) para el certificado ACME que viene a continuación (`TALAIA_ACME_PROVIDER`, `TALAIA_ACME_EMAIL`; el núcleo las ignora por ahora). El CI ocupa el 8443 y comprueba que el instalador elige otros puertos.
- El núcleo escucha en dos puertos: el del panel (`panel_listen` / `TALAIA_PANEL_LISTEN`, puerto público `panel_port`) sirve el panel, la API y el WebSocket de la terminal, y el de los agentes (`agent_listen` / `TALAIA_AGENT_LISTEN`, puerto público `agent_port`) sirve el flujo de los agentes, `/enroll`, `/renew`, `/ca.pem`, `/install.sh` y `/agent/*`. Ninguno responde a las rutas del otro. `listen`, `public_port`, `TALAIA_LISTEN` y `TALAIA_PORT` se sustituyen por estos ajustes; la orden de instalación que se muestra para los agentes usa el puerto de agentes y los orígenes permitidos el del panel. Compose publica los dos puertos (8443 y 8444 por defecto) y `packaging/core/install.sh` gana `--agent-port`; `--port` pasa a valer 8443 por defecto.
- Nuevo aspecto del panel: tipografías autoalojadas (Familjen Grotesk, IBM Plex Sans y JetBrains Mono, que estaban declaradas pero nunca se cargaban), marco con barra lateral que pasa a barra superior en móvil, balizas de estado, medidores segmentados, un logotipo nuevo de atalaya, un acceso rediseñado con un haz de luz, un favicon y animaciones que respetan `prefers-reduced-motion`. La flota ya no muestra un panel de actividad vacío. Ninguna imagen es una URL `data:`, así que el panel funciona con su CSP estricta.
- Fase 2 (iniciada): acciones tipadas sobre contenedores de punta a punta. El contrato añade `ContainerAction` y `JobEvent`; el agente ejecuta iniciar, parar, reiniciar y eliminar por la API del motor de Docker (referencias validadas, sin shell, un id de trabajo repetido recibe el primer resultado); el núcleo encola comandos por agente, guarda los trabajos (tabla `job`, los estados solo avanzan) y expone `GET /api/v1/servers/{id}/containers`, `POST /api/v1/servers/{id}/containers/{container}/actions` y `GET /api/v1/jobs/{id}`. Eliminar exige un `confirm` explícito, solo se puede actuar sobre contenedores que el agente haya informado y cada petición queda auditada.
- Registros de contenedores: el contrato añade `ReadLogs` y `JobEvent.output`; el agente lee las últimas líneas (de 1 a 1000, stdout y stderr, con marca de tiempo, acotadas a 256 KiB) por la API del motor de Docker; el núcleo expone `POST /api/v1/servers/{id}/containers/{container}/logs` y devuelve el texto en la salida del trabajo. Solo se audita el hecho de leer, nunca el texto.
- Stacks Compose gestionados por Talaia, en `/opt/talaia/stacks/<nombre>/` (`compose.yaml` y un `.env` opcional con modo 0600). El agente valida el proyecto con `docker compose config` antes de reemplazar ningún fichero, escribe de forma atómica, rechaza los enlaces simbólicos y ejecuta docker compose con argumentos fijos y un entorno reducido (sin shell); su inventario lista los stacks gestionados. El núcleo expone `GET /api/v1/servers/{id}/stacks`, `PUT …/stacks/{nombre}`, `POST …/stacks/{nombre}/read` y `POST …/stacks/{nombre}/actions` (up, down, pull, restart, remove; down y remove exigen `confirm`). La auditoría guarda solo los hashes de los ficheros, nunca su contenido.
- Uso en vivo de los contenedores: el agente muestrea `docker stats` de cada contenedor en marcha cada 15 segundos con su propio temporizador (CPU como la calcula docker stats, memoria sin la caché recuperable, bytes de red) y lo envía; el núcleo guarda la última muestra y solo la muestra en la lista de contenedores mientras es reciente (menos de un minuto) y el contenedor corre; la pantalla de servidor dibuja un medidor de CPU y otro de RAM en cada contenedor en marcha.
- Sección «Imágenes, volúmenes y redes» en la pantalla de servidor: pestañas por tipo, cada recurso marcado como en uso, sin usar o del sistema, el espacio que liberarían las imágenes sin usar, un atajo **Seleccionar lo que no se usa** y una confirmación (con aviso de datos en los volúmenes) antes de eliminar nada; los que Docker rechaza se listan después. En ca/es/en.
- Recursos de Docker: el agente lista imágenes, volúmenes y redes indicando si algún contenedor las usa (redes del sistema marcadas) y las elimina una a una, nunca con force, de modo que Docker rechaza por sí mismo lo que está en uso; el núcleo expone `POST /api/v1/servers/{id}/resources/list` y `…/resources/remove` (`confirm` explícito, como máximo 200 por petición, ids validados por tipo, cada eliminación auditada con lo que se pidió).
- Adopción de proyectos Compose en marcha: `POST /api/v1/servers/{id}/stacks/{nombre}/adopt` y la acción `forget` de stacks. El agente encuentra el proyecto entre sus contenedores y anota dónde están su único compose y su directorio; no se copia ni se reinicia nada, así que las rutas relativas y los datos siguen funcionando, y compose se ejecuta contra los ficheros originales. Eliminar un stack adoptado se rechaza; dejar de gestionarlo borra solo la anotación. El núcleo envía solo el nombre, nunca una ruta.
- Bloque de proyectos en marcha en la sección de stacks: los proyectos Compose que Talaia aún no gestiona, cada uno con un botón **Gestionar con Talaia**; los stacks adoptados llevan la etiqueta *adoptado* y ofrecen **Dejar de gestionar** (con confirmación) en lugar de eliminar. En ca/es/en.
- Sección de stacks en la pantalla de servidor: los stacks gestionados con el estado de sus contenedores, iniciar, actualizar imágenes, reiniciar, parar y eliminar (parar y eliminar piden confirmación), un editor de **Nuevo stack** y otro de **Editar** para `compose.yaml` y `.env` con **Validar y guardar** y **Guardar e iniciar**. Un proyecto rechazado muestra el mensaje de docker compose y mantiene abierto el editor. En ca/es/en.
- Visor de registros en la pantalla de servidor: un botón **Registros** por contenedor abre sus últimas líneas en una consola oscura (actualizar y cerrar), en ca/es/en.
- `install.sh --upgrade` y `talaia-core server upgrade-command`: actualizan el agente ya instalado en un servidor (misma descarga comprobada por hash, conserva su configuración e identidad, sin token).
- Pantalla de servidor: pulsa el nombre de un servidor en la flota para ver sus contenedores (estado, imagen, stack) e iniciarlos, pararlos, reiniciarlos o eliminarlos. Cada acción es un trabajo que la pantalla sigue hasta que acaba y entonces recarga la lista; eliminar pide confirmación en la propia fila. El agente reenvía su inventario en cuanto termina un trabajo. En ca/es/en.
- Decisiones de la fase 0 cerradas: PostgreSQL + TimescaleDB, React + TypeScript, Go 1.26, Node.js 24 LTS con pnpm.
- La sesión que espera el segundo factor dura 15 minutos en el primer acceso (antes 5), suficiente para instalar una aplicación de autenticación y escanear el QR; quien ya tiene el segundo factor mantiene 5 minutos. La pantalla de acceso indica ahora cuándo ha caducado la sesión.

### Corregido

- Un agente que ya tenía una identidad de otro núcleo ignoraba el nuevo token de enrolamiento y seguía sin poder conectar. Cuando `install.sh` deja un token y la CA guardada no coincide con la huella configurada, el agente descarta la identidad antigua y se enrola de nuevo.
- Instalador: al relanzarlo ya no toma su propio contenedor `acme-dns`, que una ejecución anterior dejó en marcha, por un proceso que ocupa el puerto 53.
- `docker compose up --build` con el perfil `acme-dns` fallaba en una máquina limpia con «pull access denied for talaia-core»: el servicio DNS de ACME usa la imagen del núcleo pero no decía cómo construirla. Ahora los dos servicios comparten la construcción y el CI arranca la pila con ese perfil desde cero.
- ACME: la cuenta y el certificado se guardan por directorio ACME (`<data_dir>/acme/<host>/`), así que staging y producción nunca se mezclan y una cuenta registrada en uno no se reutiliza en el otro.
- Comprobación de actualizaciones: un commit cuyo CI seguía en marcha en la primera comprobación ya no se ofrecía nunca (el ETag de la cabeza convertía las siguientes en un 304), y a una versión más nueva que el último commit en verde se le ofrecía volver atrás. Con ACME, el panel abierto por dirección IP recibe ahora el certificado de la CA propia, que la cubre, en vez del público.
- Una acción de stack que tarda más de dos minutos (un `pull` o un `up` lentos) ya no aparece como caducada mientras sigue en marcha: cada tipo de trabajo espera ahora algo más de lo que le da el agente.
- Un trabajo cuya salida o mensaje se cortaba a mitad de un carácter ya no se pierde: el agente y el core mantienen el texto en UTF-8 válido.
- Validar un stack adoptado editado con el `.env` vacío ya no usa el `.env` antiguo de su directorio, que al guardar se borra.
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

[Sin publicar]: https://github.com/catlinux/Talaia/compare/v0.3.22...HEAD
[0.3.22]: https://github.com/catlinux/Talaia/releases/tag/v0.3.22
[0.3.21]: https://github.com/catlinux/Talaia/releases/tag/v0.3.21
[0.3.20]: https://github.com/catlinux/Talaia/releases/tag/v0.3.20
[0.3.19]: https://github.com/catlinux/Talaia/releases/tag/v0.3.19
[0.3.18]: https://github.com/catlinux/Talaia/releases/tag/v0.3.18
[0.3.17]: https://github.com/catlinux/Talaia/releases/tag/v0.3.17
[0.3.16]: https://github.com/catlinux/Talaia/releases/tag/v0.3.16
[0.3.15]: https://github.com/catlinux/Talaia/releases/tag/v0.3.15
[0.3.14]: https://github.com/catlinux/Talaia/releases/tag/v0.3.14
[0.3.13]: https://github.com/catlinux/Talaia/releases/tag/v0.3.13
[0.3.12]: https://github.com/catlinux/Talaia/releases/tag/v0.3.12
[0.3.11]: https://github.com/catlinux/Talaia/releases/tag/v0.3.11
[0.3.10]: https://github.com/catlinux/Talaia/releases/tag/v0.3.10
[0.3.9]: https://github.com/catlinux/Talaia/releases/tag/v0.3.9
[0.3.8]: https://github.com/catlinux/Talaia/releases/tag/v0.3.8
[0.3.7]: https://github.com/catlinux/Talaia/releases/tag/v0.3.7
[0.3.6]: https://github.com/catlinux/Talaia/releases/tag/v0.3.6
[0.3.5]: https://github.com/catlinux/Talaia/releases/tag/v0.3.5
[0.3.4]: https://github.com/catlinux/Talaia/releases/tag/v0.3.4
[0.3.3]: https://github.com/catlinux/Talaia/releases/tag/v0.3.3
[0.3.2]: https://github.com/catlinux/Talaia/releases/tag/v0.3.2
[0.3.1]: https://github.com/catlinux/Talaia/releases/tag/v0.3.1
[0.3.0]: https://github.com/catlinux/Talaia/releases/tag/v0.3.0
[0.2.15]: https://github.com/catlinux/Talaia/releases/tag/v0.2.15
[0.2.14]: https://github.com/catlinux/Talaia/releases/tag/v0.2.14
[0.2.13]: https://github.com/catlinux/Talaia/releases/tag/v0.2.13
[0.2.12]: https://github.com/catlinux/Talaia/releases/tag/v0.2.12
[0.2.11]: https://github.com/catlinux/Talaia/releases/tag/v0.2.11
[0.2.10]: https://github.com/catlinux/Talaia/releases/tag/v0.2.10
[0.2.9]: https://github.com/catlinux/Talaia/releases/tag/v0.2.9
[0.2.8]: https://github.com/catlinux/Talaia/releases/tag/v0.2.8
[0.2.7]: https://github.com/catlinux/Talaia/releases/tag/v0.2.7
[0.2.6]: https://github.com/catlinux/Talaia/releases/tag/v0.2.6
[0.2.5]: https://github.com/catlinux/Talaia/releases/tag/v0.2.5
[0.2.4]: https://github.com/catlinux/Talaia/releases/tag/v0.2.4
[0.2.3]: https://github.com/catlinux/Talaia/releases/tag/v0.2.3
[0.2.2]: https://github.com/catlinux/Talaia/releases/tag/v0.2.2
[0.2.1]: https://github.com/catlinux/Talaia/releases/tag/v0.2.1
[0.0.1]: https://github.com/catlinux/Talaia/releases/tag/v0.0.1
