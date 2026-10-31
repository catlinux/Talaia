# Seguridad

[Read in English](../en/security.md) · [Volver al README](../../README.es.md)

Qué puede hacer el panel con tus servidores, qué lo protege y qué se graba. El diseño está en
[arquitectura.md](arquitectura.md); los pasos de instalación, en [instalacion.md](instalacion.md).
La guía de uso de la pantalla del servidor es [servidor.md](servidor.md).

## Administradores y operadores de servidor son root en los servidores que gestionan

El rol de **administrador** está protegido por contraseña y un segundo factor (TOTP). **Esa cuenta equivale
a root en cada servidor que gestiona.** Los demás roles reducen el alcance, pero cualquier rol que pueda editar
Compose, ejecutar órdenes del host o abrir una terminal privilegiada sigue equivaliendo a root dentro de su
ámbito. Las reglas están en [permisos.md](permisos.md). Es una decisión de diseño, no un descuido, porque varias
cosas que ofrece el panel ya dan root:

- **Un archivo Compose se ejecuta como root en el servidor.** Quien pueda editarlo puede montar `/`, arrancar un
  contenedor `privileged`, usar `pid: host` o `network_mode: host`, añadir capacidades o construir una imagen.
  Talaia no filtra el contenido: una lista de campos peligrosos solo daría falsa seguridad, porque hay demasiadas
  vías. El editor de stacks lo dice en una nota que no se puede cerrar.
- **Adoptar un proyecto existente** ejecuta su Compose, desde donde ya está, como root. El dueño de ese
  directorio puede cambiar lo que root ejecuta en el siguiente arranque, igual que antes de instalar Talaia.
- **La terminal de contenedor** abre un shell dentro de un contenedor. Si el contenedor es `privileged`,
  comparte el espacio de PID del host o monta el socket de Docker, ese shell es root en el servidor; el panel
  avisa antes de abrirlo y anota `privileged` en el registro de auditoría.
- **La terminal del anfitrión** abre un shell de inicio de sesión de root en el propio servidor. Necesita
  confirmación y un código TOTP nuevo, se graba como una terminal de contenedor y queda auditada. Véase
  [servidor.md](servidor.md).
- **Las actualizaciones desde el panel** las aplica un ayudante con privilegios de root en la máquina del núcleo
  ([instalacion.md](instalacion.md), sección 5). El núcleo solo puede pedir un commit de `origin/main` más nuevo
  que el que corre.

Cuida las cuentas de administrador y de operador de servidor: contraseñas largas y únicas, una aplicación de
autenticación que controles y nada de compartirlas. Los roles y ámbitos por usuario ya existen, pero los permisos
de editar stacks, adoptar proyectos y abrir terminales siguen equivaliendo a root y nunca se conceden a un lector
ni a un operador de app.

## Métodos de acceso

La base es la contraseña del administrador más TOTP, con los códigos de recuperación como vía de vuelta. Hay dos
métodos opcionales que hacen más rápido el acceso habitual:

- **Un PIN en un dispositivo de confianza.** Desde **Ajustes > Cuenta**, un navegador con sesión iniciada se
  convierte en un dispositivo de confianza con un PIN de 6 a 12 dígitos y la contraseña actual. El núcleo le
  envía una cookie `__Host-talaia_device` de 180 días y guarda solo su hash; el PIN no sirve sin esa cookie y se
  guarda con hash. Cinco PIN erróneos desactivan el dispositivo. El TOTP se vuelve a pedir cuando se cierra el
  navegador o después de `device_mfa_hours` (12 por defecto, rango de 1 a 168), y un usuario puede tener como
  máximo 10 dispositivos de confianza. Cambiar la contraseña, o restablecer el secreto TOTP, elimina todos los
  dispositivos de confianza.
- **Claves de acceso.** Credenciales WebAuthn con verificación del usuario (huella, cara o PIN del dispositivo).
  Una clave cuenta como la contraseña más el segundo factor, así que abre una sesión completa sin TOTP. Un
  usuario puede tener como máximo 10 claves. Necesitan HTTPS con un nombre DNS en `public_hostnames`; una
  dirección IP no puede ser el relying party, y con la CA propia de Talaia algunos navegadores pueden
  rechazarlas. Los desafíos caducan a los cinco minutos, y un contador de firmas que retrocede desactiva la
  clave.

- **Inicio de sesión único (OIDC).** Un proveedor OpenID Connect configurado puede sustituir la contraseña de
  un usuario existente cuyo correo verificado coincida. El segundo factor sigue siendo obligatorio. Los fallos
  del proveedor vuelven a un error genérico del acceso y se auditan; véase [permisos.md](permisos.md).
Estos métodos son adicionales: la contraseña, el TOTP y los códigos de recuperación siguen siendo la base y la
vía de recuperación. Configuración, uso, eliminación y problemas frecuentes: [acceso.md](acceso.md).

## Usuarios, invitaciones y denegaciones

- Hay cinco roles fijos que se pueden asignar en los ámbitos organización, grupo, servidor, stack o
  contenedor. Un permiso solo cuenta cuando el ámbito es al menos su nivel mínimo; un operador de servidor
  asignado a un stack nunca llega al host. Véase [permisos.md](permisos.md).
- Quien no puede ver un objeto recibe **404**, no una pista de que existe. Quien lo ve pero no tiene la
  operación recibe **403**. El panel solo esconde controles; el núcleo toma todas las decisiones.
- Cada denegación se audita como `authz_denied` con el actor, el permiso, el método, la ruta, el resultado y
  el ámbito; las denegaciones de claves de API incluyen además el id de la clave. La respuesta nunca
  contiene el motivo.
- Un enlace de invitación es de un solo uso, caduca a las 72 horas y el token se guarda con hash. Quien
  consiga el enlace antes de usarlo puede reclamar la cuenta, así que envíalo por un canal de confianza.
  Talaia no envía correo. La persona invitada pone una contraseña y debe configurar TOTP antes de tener una
  sesión completa.
- Debe quedar al menos un administrador de organización activo. Desactivar al último, o quitar o degradar
  la última asignación de administrador de organización (directa o mediante un equipo), se rechaza con
  `409 last_admin`; nadie puede desactivar su propia cuenta.

## Claves de API

- Las claves de API son credenciales con ámbito para scripts. Se muestran una vez, se guardan como hash
  SHA-256 y caducan a los 90 días por defecto (365 como máximo).
- Los permisos efectivos son la intersección de las asignaciones de la clave y las actuales de su dueño. Si
  el dueño pierde acceso, la clave también.
- Una clave nunca puede abrir una terminal de contenedor o del anfitrión, gestionar usuarios, equipos,
  grupos, claves ni ajustes, reiniciar un servidor ni usar los endpoints de acceso. Las rutas marcadas como
  `noAPIKey` rechazan las claves aunque sus asignaciones de rol las permitieran.
- Las claves usan `Authorization: Bearer tlk_…`, no necesitan cookie ni token CSRF y están limitadas a 600
  peticiones por minuto por clave, con un límite adicional por IP. Crear una exige un código TOTP reciente.
- Trata la clave como una contraseña: no la pongas en URL, logs ni historial de shell, y prefiere
  `TALAIA_API_KEY` o `talaiactl login` antes que `--api-key`, que aparece en la lista de procesos. Revócala
  de inmediato si se filtra.

## Inicio de sesión único (OIDC)

- El proveedor sustituye solo la contraseña. TOTP o una clave de acceso siguen siendo obligatorios, así que
  un proveedor de identidad comprometido por sí solo no abre el panel.
- El claim de correo debe estar verificado y coincidir con un usuario activo existente. No hay alta
  automática de cuentas. En el primer acceso se vincula el `sub` del proveedor a ese usuario; un subject
  distinto para el mismo correo se rechaza.
- El flujo usa descubrimiento, código de autorización con PKCE, `state` y `nonce` en una cookie sellada de
  10 minutos, y verifica el ID token. El secreto de cliente es de solo escritura y se guarda cifrado.
- Solo se aceptan emisores `https`, salvo `http://localhost` o `http://127.0.0.1` en desarrollo.
- Los fallos redirigen a un error genérico del acceso y se auditan como `oidc_login_denied`; no revelan si
  existe la cuenta. Quien controle el proveedor puede hacerse pasar por sus usuarios después del segundo
  factor, así que protege el proveedor con el mismo cuidado que el panel.

## Acceso a las grabaciones de terminal

- Las grabaciones solo están disponibles con `recordings.view` en ámbito de organización, desde
  **Auditoría > Grabaciones**. Las claves de API nunca pueden listarlas ni descargarlas.
- Descargar un `.cast` se audita como `recording_download`. Una grabación puede contener contraseñas o
  tokens impresos en pantalla; trátala como dato sensible y guárdala en consecuencia.
- El panel no tiene reproductor integrado. Usa `asciinema play archivo.cast` en local. La retención es de 90
  días por defecto y los trozos se cifran con la clave de secretos.

## Actualizaciones de los agentes

El núcleo puede ordenar a un agente que se actualice (la orden es tipada y lleva el SHA-256 del paquete). El agente
descarga `/agent/talaia-agent_<arq>.deb` por su canal con TLS mutuo, rechaza una versión igual o anterior y un
paquete cuyo hash no coincide, y lo instala con `apt-get` en una unidad de systemd aparte. Es código que se ejecuta
como root en cada servidor, elegido por quien controla el núcleo: el hash protege de corrupción y manipulación en
tránsito, no de un núcleo comprometido, la misma confianza que el comando de instalación. Cada orden se audita como
`agent_update`.

## Qué protege el panel y los agentes

- **Acceso:** contraseña y TOTP, PIN opcional en dispositivos de confianza, claves de acceso y OIDC para
  usuarios existentes, límite de intentos, bloqueo tras fallos repetidos, sesiones que caducan y protección de
  cada cambio frente a peticiones de otros sitios (comprobación del origen).
- **Solo órdenes tipadas.** El núcleo nunca envía un comando de shell a un agente. Envía mensajes cerrados y
  validados (iniciar un contenedor, leer registros, escribir un stack, abrir una terminal…) y el agente los
  vuelve a comprobar. Los nombres de contenedores y stacks y las rutas se validan antes de usarlos.
- **Las acciones destructivas piden confirmación expresa** en el panel y en la propia petición. Eliminar
  imágenes, volúmenes y redes nunca fuerza: Docker rechaza lo que está en uso.
- **Los agentes se conectan hacia fuera** por TLS mutuo con un certificado de la CA propia del núcleo, atado a
  un solo servidor. Un agente solo puede informar y recibir órdenes de su propio servidor. El puerto de los
  agentes presenta siempre el certificado de la CA interna, aunque el panel use uno público, así que quien se
  haga con tu DNS no puede hacerse pasar por el núcleo.
- **El certificado no necesita ninguna clave del proveedor de DNS** con la configuración recomendada: la zona
  delega solo `_acme-challenge.<nombre>` en el servidor, y `acme-dns` lo contesta en el puerto 53 desde su propio
  contenedor de solo lectura, sin la base de datos, los secretos ni los datos del núcleo. Solo contesta ese nombre,
  nunca hace recursión y limita cada red de origen. Véase [dns-delegation.md](dns-delegation.md).
- **Todo lo que hace el panel queda auditado** con nombres, ids y huellas; el registro de auditoría nunca guarda
  contenido de ficheros, texto de registros, el código TOTP, el texto de búsqueda del journal, las listas de
  paquetes ni lo que se escribe en una terminal. Las comprobaciones de autorización denegadas se registran como
  `authz_denied`.
- **Los roles con ámbito se aplican en el núcleo.** El panel esconde controles, pero la API comprueba cada ruta
  contra las asignaciones efectivas; un objeto oculto devuelve 404 y una operación visible sin permiso, 403.

## Sesiones de terminal: grabación y retención

Una sesión de terminal siempre se graba, y no se abre si no se puede grabar. Vale para las terminales de
contenedor y para la del anfitrión.

- **Qué se graba:** la *salida* de la terminal y sus cambios de tamaño, en formato asciicast v2. **Lo que
  escribes no se graba**, pero lo que el shell devuelve con eco forma parte de la salida: una contraseña
  escrita en un prompt que la muestra, o impresa por un comando, queda en la grabación. La interfaz lo recuerda
  de forma permanente.
- **Dónde:** en la base de datos del núcleo, en trozos que se cifran uno a uno con la clave de `secrets.key`
  (volumen `core-data`). Sin ese fichero las grabaciones no se pueden leer, y lo mismo vale para una copia de la
  base de datos sola.
- **Retención:** 90 días por defecto. Se cambia con `TALAIA_TERMINAL_RECORDING_DAYS` (o `terminal_recording_days`
  en el fichero de configuración). Las más antiguas se borran al arrancar el núcleo y cada 24 horas.
- **Límites:** como máximo 4 sesiones por servidor, compartidas por shells de contenedor y de anfitrión; una
  por usuario y contenedor, y una de anfitrión por usuario y servidor, con un máximo de 2 de anfitrión por
  agente; una sesión se cierra tras 15
  minutos sin entrada ni salida, a las 4 horas o cuando su grabación llega a 50 MiB. Cada cierre tiene su motivo
  en el registro de auditoría (`user`, `idle`, `max_duration`, `recording_limit`, `slow_client`,
  `agent_offline`, `session_revoked`, `exited`).
- **Para abrir una** hace falta una sesión completa y un ticket de un solo uso que caduca a los 30 segundos y
  pertenece a esa sesión del navegador, y la conexión debe venir de un origen permitido. Cerrar o revocar la
  sesión del panel cierra sus terminales en menos de un minuto.

El panel no tiene reproductor integrado. Quien tenga `recordings.view` puede listar las sesiones y descargar el
archivo `.cast` en **Auditoría > Grabaciones**; las descargas se auditan. Para listarlas y exportarlas desde la
consola, en la máquina del núcleo y en `packaging/core`:

```bash
sudo docker compose run --rm -T core terminal list
sudo docker compose run --rm -T core terminal export <id-de-sesión> > sesion.cast
asciinema play sesion.cast
```

`terminal list` imprime una línea por sesión, la más reciente primero: id, hora de apertura, estado o motivo de
cierre, usuario, contenedor o anfitrión, duración y bytes grabados, más `root-equivalent` en los contenedores privilegiados.
`terminal export` descifra una grabación y escribe el fichero `.cast` por la salida estándar.

## Acciones sobre el servidor entero (fase 3)

Un administrador, o un operador de servidor cuyo ámbito llega al servidor, puede cambiar la máquina entera, no
solo sus contenedores:

- **Actualizaciones de paquetes** ejecutadas como root que pueden sustituir bibliotecas, servicios y el
  kernel. Hay actualización solo de seguridad o de todo lo pendiente; nunca eliminan paquetes y conservan
  los ficheros de configuración locales. Solo puede haber una actualización apt o un reinicio a la vez. La
  tarea corre en una unidad transitoria de systemd para sobrevivir a un reinicio del agente, y el panel
  muestra su registro en directo.
- **Control de servicios** para iniciar, parar, reiniciar, recargar, habilitar o deshabilitar servicios
  systemd. Parar, reiniciar y deshabilitar piden confirmación; los servicios críticos para el acceso
  muestran un aviso más fuerte. `talaia-agent.service` no se puede parar, reiniciar ni deshabilitar desde
  el panel.
- **Reinicio** con el nombre de máquina que informa el agente; el agente lo comprueba otra vez y programa
  el reinicio. Talaia nunca apaga una máquina.
- **La terminal del anfitrión** abre un shell de root grabado en el servidor mediante un PTY local.
  Necesita confirmación y un código TOTP nuevo, con la misma protección contra reutilización que el acceso
  y el cambio de contraseña. Está limitada a 2 sesiones por agente y una por usuario y servidor, comparte
  el límite de 4 sesiones por servidor y los límites de 15 minutos sin actividad, 4 horas y 50 MiB de
  grabación.

Todo son órdenes tipadas; el núcleo no envía una línea de shell. La guía de uso, con los pasos seguros de
actualización y reinicio, es [servidor.md](servidor.md).

### Qué graba la fase 3 y qué no graba

El registro de auditoría guarda la acción, el actor, la IP, el resultado e identificadores como el
servidor, la unidad o la tarea. Las acciones de la fase 3 son `apt_status`, `apt_upgrade`,
`host_reboot`, las acciones `service_*`, `journal_read`, `host_terminal_open` y
`host_terminal_close`.

- **Nunca el código TOTP.** El código se comprueba y se descarta; no se escribe en la auditoría, en los
  logs de la aplicación ni en la grabación.
- **Nunca el texto del journal ni el de búsqueda.** `journal_read` guarda la unidad y el arranque, no las
  líneas ni la búsqueda literal.
- **Nunca las listas de paquetes.** `apt_status` y `apt_upgrade` guardan el modo, las opciones y el id de
  la tarea, no los nombres de paquetes ni la salida del comando.
- **Nunca las pulsaciones.** Una grabación de terminal guarda la salida y los cambios de tamaño; lo que
  escribes no se guarda, pero lo que el shell devuelve con eco forma parte de la salida. El prompt del TOTP
  no se refleja en la grabación.
- **Leer la auditoría no se audita.** Si no, cada actualización de la pantalla de Auditoría añadiría un
  evento.

### Desactivar la terminal del anfitrión

Las terminales del anfitrión están activadas por defecto en Linux. Para apagarlas en un servidor, añade
esto a `/etc/talaia/agent.toml` y reinicia el agente:

```toml
[terminal]
host = false
```

```bash
sudo systemctl restart talaia-agent
```

La variable de entorno `TALAIA_AGENT_TERMINAL_HOST=false` tiene el mismo efecto y prioridad sobre el
fichero. El agente informa de la capacidad como falsa, así que el panel desactiva el botón. Los agentes
antiguos también informan falso y necesitan actualizarse.

## Tráfico saliente: comprobaciones, notificaciones y protección SSRF

Las comprobaciones y los canales de notificación hacen que el núcleo abra conexiones salientes. El destino es
entrada no confiable, así que Talaia aplica una protección en el momento de conectar:

- Los destinos HTTP deben usar `http` o `https`, tener un host y no contener credenciales en la URL. Las
  variantes numéricas no canónicas de IPv4 se rechazan. Cada redirección se comprueba de nuevo y la cadena
  se limita a tres redirecciones.
- El DNS se resuelve antes de conectar y se comprueba cada dirección real del socket. Así, una respuesta de
  DNS rebinding no puede convertir un nombre público en una dirección interna después de la validación.
- Por defecto se bloquean las direcciones de bucle local, privadas RFC 1918, NAT de operador, link-local,
  únicas locales de IPv6, rangos de pruebas, documentación y reservados, multicast, no especificadas y de
  difusión.
- El interruptor **Permitir que las comprobaciones y las notificaciones lleguen a redes privadas** de
  **Alertas** relaja los destinos privados, de bucle local y NAT de operador. Las direcciones
  link-local, los rangos reservados de documentación, multicast/difusión y los metadatos siguen bloqueados.
- Los endpoints de metadatos de nube `169.254.169.254` y `fd00:ec2::254` están **siempre** bloqueados porque
  pueden exponer credenciales de instancia; el interruptor nunca los permite.
- La protección también se aplica a las conexiones SMTP. Los clientes HTTP no usan los proxies del entorno,
  así que un proxy no puede saltarse la comprobación.

Las reglas completas y cómo probar un destino bloqueado están en
[monitorizacion.md](monitorizacion.md#protección-del-tráfico-saliente).

## Secretos de los canales de notificación

Los secretos de los canales son de solo escritura y se sellan con AES-256-GCM usando la misma
`data_dir/secrets.key` que protege los secretos TOTP y el secreto de cliente de OIDC. La API devuelve solo
`has_secret`; el panel nunca recibe, muestra ni registra el valor guardado. Editar un canal sin tocar el
secreto no envía ningún campo de secreto y conserva el existente; **Sustituir** envía solo el valor nuevo. Los
campos secretos usan `type="password"` y `autocomplete="new-password"`.

Los errores de envío y el resultado de **Enviar prueba** se censuran contra cada valor secreto antes de
guardarlos en el estado del canal o devolverlos al panel. El SMTP nunca se envía en
texto plano: solo se aceptan STARTTLS y TLS implícito. Discord solo acepta una URL oficial de webhook. El HMAC
opcional del webhook firma la marca de tiempo y el cuerpo exacto con HMAC-SHA256 en `X-Talaia-Signature`; el
esquema de verificación está en [monitorizacion.md](monitorizacion.md#firma-del-webhook).

Perder `data_dir/secrets.key` hace ilegibles los secretos sellados de los canales. Haz copia del volumen
`core-data` y no imprimas un secreto para diagnosticar un canal.

## Informar de una vulnerabilidad

No abras un issue público: sigue [SECURITY.es.md](../../SECURITY.es.md).
