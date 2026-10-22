# Seguridad

[Read in English](../en/security.md) · [Volver al README](../../README.es.md)

Qué puede hacer el panel con tus servidores, qué lo protege y qué se graba. El diseño está en
[arquitectura.md](arquitectura.md); los pasos de instalación, en [instalacion.md](instalacion.md).
La guía de uso de la pantalla del servidor es [servidor.md](servidor.md).

## El administrador es root en cada servidor gestionado

En esta versión hay un solo rol: el administrador, protegido por contraseña y un segundo factor (TOTP). **Esa
cuenta equivale a root en cada servidor que gestiona.** Es una decisión de diseño, no un descuido, porque varias
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

Cuida esa cuenta: una contraseña larga y única, una aplicación de autenticación que controles y no compartirla.
Los roles y abasto por usuario (por ejemplo, acceso a un solo stack) llegan en una fase posterior; cuando lleguen,
los permisos de editar stacks, adoptar proyectos y abrir terminales se marcarán como equivalentes a root y no se
concederán por defecto.

## Métodos de acceso

La base es la contraseña del administrador más TOTP, con los códigos de recuperación como vía de vuelta. Hay dos
métodos opcionales que hacen más rápido el acceso habitual:

- **Un PIN en un dispositivo de confianza.** Desde **Ajustes > Acceso**, un navegador con sesión iniciada se
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

Los dos métodos son adicionales: la contraseña, el TOTP y los códigos de recuperación siguen siendo la base y la
vía de recuperación. Configuración, uso, eliminación y problemas frecuentes: [acceso.md](acceso.md).

## Actualizaciones de los agentes

El núcleo puede ordenar a un agente que se actualice (la orden es tipada y lleva el SHA-256 del paquete). El agente
descarga `/agent/talaia-agent_<arq>.deb` por su canal con TLS mutuo, rechaza una versión igual o anterior y un
paquete cuyo hash no coincide, y lo instala con `apt-get` en una unidad de systemd aparte. Es código que se ejecuta
como root en cada servidor, elegido por quien controla el núcleo: el hash protege de corrupción y manipulación en
tránsito, no de un núcleo comprometido, la misma confianza que el comando de instalación. Cada orden se audita como
`agent_update`.

## Qué protege el panel y los agentes

- **Acceso:** contraseña y TOTP, PIN opcional en dispositivos de confianza y claves de acceso, límite de
  intentos, bloqueo tras fallos repetidos, sesiones que caducan y protección de cada cambio frente a peticiones
  de otros sitios (comprobación del origen).
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
  paquetes ni lo que se escribe en una terminal.

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

El panel todavía no tiene reproductor de grabaciones. Para listarlas y reproducirlas, en la máquina del núcleo y
en `packaging/core`:

```bash
sudo docker compose run --rm -T core terminal list
sudo docker compose run --rm -T core terminal export <id-de-sesión> > sesion.cast
asciinema play sesion.cast
```

`terminal list` imprime una línea por sesión, la más reciente primero: id, hora de apertura, estado o motivo de
cierre, usuario, contenedor o anfitrión, duración y bytes grabados, más `root-equivalent` en los contenedores privilegiados.
`terminal export` descifra una grabación y escribe el fichero `.cast` por la salida estándar.

## Acciones sobre el servidor entero (fase 3)

La misma cuenta de administrador puede ahora cambiar la máquina entera, no solo sus contenedores:

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

## Informar de una vulnerabilidad

No abras un issue público: sigue [SECURITY.es.md](../../SECURITY.es.md).
