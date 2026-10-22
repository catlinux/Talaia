# La pantalla del servidor

[Read in English](../en/server.md) · [Volver al README](../../README.es.md)

La pantalla del servidor gestiona un VPS entero, no solo los contenedores que corren en él. Tiene
pestañas **Resumen**, **Contenedores**, **Servicios**, **Registros** y **Actualizaciones**, además de un
botón **Terminal** para abrir un shell de root en el propio servidor.

La cuenta de administrador equivale a root en cada servidor gestionado. Lee
[seguridad.md](seguridad.md) antes de usar la terminal del anfitrión, actualizar paquetes o cambiar
servicios. Los detalles de implementación están en [arquitectura.md](arquitectura.md); los pasos de
instalación, en [instalacion.md](instalacion.md).

## Antes de empezar

- Entra con una sesión completa. El panel necesita el segundo factor; una sesión pendiente no puede usar
  estas operaciones.
- Mantén una vía de acceso fuera del panel (la consola del proveedor o SSH) hasta haber terminado. Un
  cambio erróneo de red o de SSH puede quitarte el acceso al panel.
- El servidor debe ejecutar `talaia-agent` con las herramientas de la función que quieras usar:
  `apt-get` y `apt-mark` para las actualizaciones, `systemctl` y `systemd-run` para los servicios y las
  unidades transitorias de apt, `journalctl` para los registros, y `bash` o `sh` para la terminal del
  anfitrión. La terminal del anfitrión solo está disponible en Linux.
- Un agente antiguo informa de menos capacidades. El panel muestra el botón desactivado y explica qué
  falta en lugar de enviar una orden que no entiende.

## La cabecera

La cabecera muestra siempre el nombre del servidor y, cuando el agente está conectado, un resumen de los
contenedores. También muestra la versión del agente, un botón **Actualizar agente** cuando hay un paquete
nuevo y el botón **Terminal**.

El botón **Terminal** abre el shell de root del anfitrión, no un shell de contenedor. Está desactivado
cuando el agente tiene las terminales del anfitrión desactivadas o es demasiado antiguo. La confirmación,
el código TOTP, el aviso de grabación y la franja roja **Root en <servidor>** se explican en
[La terminal del anfitrión](#la-terminal-del-anfitrión).

## Resumen

La pestaña **Resumen** es la de entrada. Muestra tres medidores de CPU, memoria y disco, seguidos de una
lista de datos con el sistema operativo, el kernel, la arquitectura, el número de CPUs, la memoria, los
contadores de red, los contenedores, los servicios, la versión del agente y la versión de Docker cuando se
conoce. El tiempo activo aparece cuando el agente lo informa.

La sección **Necesita atención** solo se muestra cuando hay algo que hacer. Enlaza con la pestaña que
resuelve el aviso:

- hace falta reiniciar o hay actualizaciones de seguridad pendientes: **Actualizaciones**;
- el disco pasa del 85 %: **Contenedores**, donde está la limpieza de recursos de Docker;
- hay servicios systemd fallidos: **Servicios**;
- hay un paquete nuevo del agente: **Actualizaciones**.

La lista es un atajo, no sustituye a leer la pestaña: comprueba siempre qué va a cambiar antes de
confirmar una acción.

## Contenedores

La pestaña **Contenedores** conserva la gestión de contenedores y stacks de la fase anterior. Muestra los
contenedores del servidor, los stacks Compose que Talaia gestiona y las imágenes, volúmenes y redes de
Docker que se pueden limpiar. Los registros y la terminal del contenedor siguen disponibles aquí.

## Servicios

La pestaña **Servicios** lista los **servicios** systemd que ha informado el agente, con su estado activo
y si su fichero de unidad está habilitado. Usa la búsqueda y los filtros de estado para encontrar uno.

Las acciones disponibles son:

- **Iniciar**, **Recargar** y **Habilitar** sin una confirmación adicional;
- **Parar**, **Reiniciar** y **Deshabilitar** tras un diálogo de confirmación.

Parar, reiniciar o deshabilitar un servicio crítico para el acceso muestra un aviso adicional. La lista
fija es `ssh.service`, `sshd.service`, `docker.service`, `networking.service`,
`systemd-networkd.service` y `NetworkManager.service`. Si pierdes la conexión con el panel después de
cambiar uno de ellos, usa la consola del proveedor o SSH para recuperarlo.

`talaia-agent.service` no se puede parar, reiniciar ni deshabilitar desde el panel: cortaría la conexión
del agente con el núcleo. Talaia actualiza el agente con **Actualizar agente**. Las unidades estáticas,
enmascaradas y alias no se pueden habilitar ni deshabilitar aquí.

En esta fase los servicios son solo unidades `.service`; los timers y sockets no se gestionan como
servicios.

## Registros

La pestaña **Registros** lee el journal de systemd del servidor. Puedes filtrar por:

- **Unidad**: los servicios que ha informado el agente. Déjala vacía para todas las unidades.
- **Prioridad**: todas, errores, avisos, avisos informativos, información o depuración. Envía una
  prioridad máxima del journal (0–7).
- **Arranque**: todos los arranques, el actual o el anterior.
- **Rango de tiempo**: todo, los últimos 15 minutos, 1 hora, 24 horas, 7 días o un rango RFC 3339
  personalizado.
- **Buscar**: texto literal, hasta 200 caracteres. Es una búsqueda de texto, no una expresión regular.
- **Líneas**: 100, 200, 500 o 1000 entradas. Por defecto, 200.

Pulsa **Buscar** para aplicar los filtros. **Seguir** pide entradas nuevas cada 3 segundos usando el
cursor del journal; el panel conserva las 2000 más recientes. **Descargar** guarda las entradas que hay en
pantalla en un fichero de texto plano con la hora, la unidad, la prioridad y el mensaje.

El texto del journal y el texto de búsqueda no se escriben en el registro de auditoría. La entrada
`journal_read` guarda la unidad y el arranque, no las líneas.

## Actualizaciones

La pestaña **Actualizaciones** muestra los paquetes apt pendientes. Las actualizaciones de seguridad están
marcadas y ordenadas primero. También muestra:

- cuándo comprobó el agente por última vez y cuándo se actualizaron las listas de paquetes;
- si hace falta reiniciar, con los motivos (el fichero `/run/reboot-required` o un kernel instalado más
  nuevo que el que está en ejecución);
- los paquetes retenidos y los nombres que se actualizarían;
- un aviso cuando la actualización incluye paquetes de Docker que reiniciarán los contenedores.

**Comprobar ahora** ejecuta `apt-get update` y después pide al agente una lista nueva. Sin pulsarlo, la
pestaña muestra el último resumen que tiene el agente. Una comprobación con actualización puede tardar más
que una lectura normal.

### Actualizar paquetes con seguridad

1. Abre **Actualizaciones** y pulsa **Comprobar ahora** una vez. Lee la lista de paquetes, las marcas de
   seguridad, los paquetes retenidos y cualquier aviso de Docker.
2. Elige **Actualizar seguridad (N)** para instalar solo los paquetes cuya versión candidata viene de un
   archivo de seguridad, o **Actualizar todo (N)** para actualizar todos los pendientes.
3. Confirma el diálogo. En una actualización de seguridad el agente calcula la lista de paquetes; el
   núcleo nunca envía nombres de paquetes.
4. Mantén la pestaña abierta para seguir el registro en directo. La tarea pertenece al núcleo y al agente,
   no al navegador.

La actualización nunca elimina paquetes (`--no-remove`), conserva los ficheros de configuración locales
(`--force-confdef` y `--force-confold`) y usa un tiempo de espera de 120 segundos para el bloqueo. Se
ejecuta en una unidad transitoria de systemd llamada `talaia-apt-<id corto de la tarea>`, así que
sobrevive a un reinicio del agente; cuando el agente vuelve a arrancar, retoma la unidad e informa del
resultado. El estado y los registros están en `/var/lib/talaia/apt/` del servidor.

Solo puede haber una actualización apt o un reinicio en marcha a la vez. El núcleo rechaza otra con
**Ya hay otra operación de apt o un reinicio en marcha** mientras la anterior siga activa. Espera y lee su
registro en lugar de reintentar.

Si después de actualizar hace falta reiniciar, usa el proceso de reinicio de abajo. Algunos cambios (un
kernel nuevo o una biblioteca de un servicio) solo surten efecto tras el reinicio.

## Reiniciar el servidor

El reinicio es deliberadamente pausado: no hay un botón de apagado de un solo clic.

1. Abre **Actualizaciones** y pulsa **Reiniciar** (o el botón del aviso de reinicio).
2. Escribe en el diálogo el **nombre de máquina que informa el agente**. El panel habilita la confirmación
   solo cuando coincide, sin distinguir mayúsculas y quitando espacios sobrantes.
3. Pulsa el botón de confirmación.

El agente vuelve a comprobar el nombre, rechaza la petición mientras hay una actualización apt en marcha y
programa `systemctl reboot` cinco segundos después. Nunca apaga la máquina: un servidor apagado no se
podría volver a encender desde Talaia. El panel pierde la conexión con el agente mientras el servidor se
reinicia; la pantalla queda fuera de línea y vuelve cuando el agente reconecta. Ten a mano la consola del
proveedor por si el servidor no vuelve.

## La terminal del anfitrión

El botón **Terminal** de la cabecera abre un shell de inicio de sesión de root en el servidor a través de
un PTY local. Es la máquina entera, no un contenedor. El panel muestra una franja roja permanente **Root
en <servidor>** y la marca de grabación mientras la sesión está viva.

Para abrirla, el agente debe informar de que admite terminales del anfitrión (si no, el botón está
desactivado), y tienes que:

1. confirmar el diálogo, y
2. escribir un código TOTP actual de seis cifras. El código usa la misma protección contra reutilización
   que el acceso y el cambio de contraseña: un código ya usado se rechaza.

Elige `bash` o `sh`. El agente arranca `/bin/bash -l` o `/bin/sh -l` como root con un entorno limpio,
`HOME=/root`, el `PATH` estándar, el `LANG` del sistema y el directorio de trabajo `/root`. Al cerrar la
sesión se mata su grupo de procesos. La lista de shells es cerrada; el panel no puede enviar un shell
arbitrario.

La sesión se graba como la de un contenedor, pero solo se guardan la **salida** y los cambios de tamaño;
**las pulsaciones no se graban**. Todo lo que el shell devuelve con eco, incluida una contraseña escrita en
un prompt que la muestra o impresa por un comando, forma parte de la grabación. La sesión se audita como
`host_terminal_open` y `host_terminal_close`; el código TOTP nunca se escribe en el registro de auditoría
ni en los logs.

Límites:

- como máximo 2 sesiones de anfitrión por agente, y una sesión de anfitrión por usuario y servidor;
- como máximo 4 sesiones de terminal en total por servidor, compartidas con las de contenedor;
- una sesión se cierra tras 15 minutos sin entrada ni salida, a las 4 horas o cuando su grabación llega a
  50 MiB. El motivo queda registrado.

La grabación se guarda cifrada en la base de datos del núcleo con la retención configurada para las
grabaciones de contenedor. El panel todavía no tiene reproductor; `terminal list` y `terminal export` en
la máquina del núcleo funcionan para sesiones de contenedor y de anfitrión, y muestran una sesión de
anfitrión como `host`.

### Desactivar la terminal del anfitrión

Las terminales del anfitrión están activadas por defecto en Linux. Para desactivar las sesiones nuevas en
un servidor, edita `/etc/talaia/agent.toml`:

```toml
[terminal]
host = false
```

Después reinicia el agente:

```bash
sudo systemctl restart talaia-agent
```

La variable de entorno `TALAIA_AGENT_TERMINAL_HOST` también acepta `true` o `false`; tiene prioridad sobre
el fichero. Con systemd, añádela en un drop-in si tu instalación no la fija ya:

```bash
sudo systemctl edit talaia-agent
```

```ini
[Service]
Environment=TALAIA_AGENT_TERMINAL_HOST=false
```

Después ejecuta `sudo systemctl daemon-reload && sudo systemctl restart talaia-agent`.

El agente informa del valor nuevo en su inventario. El panel desactiva el botón con una explicación cuando
las terminales del anfitrión están apagadas; los agentes antiguos informan `false` y necesitan
actualizarse. Cambiar la opción requiere reiniciar el agente; tenlo en cuenta para las sesiones abiertas.

### Cuándo usarla

Usa la terminal del anfitrión para un cambio que no pertenece a un solo contenedor y que puedes hacer con
seguridad en una sesión grabada: inspeccionar un servicio, revisar un fichero de red o ejecutar una orden
puntual. Prefiere SSH o la consola del proveedor cuando lo que estás cambiando es el propio panel, cuando
el agente está desconectado o cuando necesitas una vía de recuperación que no dependa de Talaia. La
terminal del anfitrión es una comodidad, no un sustituto del acceso fuera de banda.

## Leer el registro de auditoría

**Auditoría** está en la barra lateral. Es una lista de solo lectura de los eventos que ha grabado el
núcleo, los más recientes primero. Cada fila muestra la hora local y la hora relativa, el actor, la acción
(traducida cuando el panel la conoce; si no, el nombre original), el resultado y el nombre o id del
servidor. **Detalles** despliega unas claves de detalle como texto; los valores nunca se interpretan como
HTML.

Filtros:

- **Acción**: todas las acciones, inicio de sesión, servidores, contenedores y stacks, stacks,
  actualizaciones, servicios, registros, terminal, terminal del anfitrión o anfitrión.
- **Actor**: nombre de usuario exacto. Escríbelo y pulsa **Filtrar** (o Intro).

**Cargar más** sigue el cursor que devuelve el núcleo. El núcleo acepta un tamaño de página de 1 a 200 y
usa 50 por defecto; la pantalla pide 50. Leer esta pantalla no se audita: inundaría el registro.

Las acciones principales de la fase 3 son `apt_status`, `apt_upgrade`, `host_reboot`, `service_start`,
`service_stop`, `service_restart`, `service_reload`, `service_enable`, `service_disable`,
`journal_read`, `host_terminal_open` y `host_terminal_close`. Las acciones de contenedores y stacks
conservan sus nombres `container_*` y `stack_*`. El registro de auditoría nunca guarda el código TOTP, las
líneas del journal, el texto de búsqueda del journal, las listas de paquetes ni las pulsaciones de la
terminal.

## Problemas frecuentes

| Síntoma | Causa y solución |
|---|---|
| Una actualización falla con un error de bloqueo de apt | Otro proceso de apt tiene el bloqueo. Espera a que terminen `apt-daily`, `unattended-upgrades` o una orden apt manual. Comprueba quién lo tiene con `sudo fuser -v /var/lib/dpkg/lock-frontend` y `sudo fuser -v /var/lib/apt/lists/lock`; no borres los ficheros de bloqueo. El agente espera hasta 120 segundos. |
| **Ya hay otra operación de apt o un reinicio en marcha** | Una actualización o un reinicio anterior sigue activo. Espera y lee el registro en directo o el estado de la unidad `talaia-apt-*` con `systemctl list-units 'talaia-apt-*' --all`. El núcleo no iniciará otra actualización apt ni otro reinicio. |
| El botón **Terminal** está desactivado | El agente tiene `[terminal] host = false`, la variable `TALAIA_AGENT_TERMINAL_HOST` vale `false`, o el agente es demasiado antiguo para informar de la capacidad. Revisa `/etc/talaia/agent.toml`, `systemctl cat talaia-agent` y la versión del agente; actualízalo y reinícialo. |
| Una terminal del anfitrión se abre y se cierra al momento | Revisa el log del agente con `sudo journalctl -u talaia-agent -n 50 --no-pager`. Un shell que falta, un código TOTP rechazado, los límites de sesión o una desconexión del agente quedan con su motivo. |
| La pestaña **Servicios** dice que el agente está desconectado | El núcleo no tiene un stream vivo de ese servidor. Comprueba `sudo systemctl status talaia-agent --no-pager` y el camino de red hasta el núcleo. |
| Se rechaza una acción sobre un servicio | La unidad no está en el último inventario, la acción necesita confirmación, o es `talaia-agent.service` con parar/reiniciar/deshabilitar. Actualiza la lista y lee el mensaje. |
| El journal no muestra entradas | Quita los filtros de unidad y búsqueda y prueba un rango de tiempo más amplio. Comprueba que `journalctl` funciona en el servidor y que el agente puede leer el journal. |
| El reinicio se aceptó pero el servidor no vuelve | El panel ya está desconectado. Usa la consola del proveedor o SSH y revisa `systemctl status talaia-agent` y los mensajes de arranque. El panel no puede encender una máquina. |
| La pantalla de auditoría está vacía | Amplía o quita los filtros de acción y actor. Leer el registro de auditoría no se audita, así que una lectura vacía no crea un evento. |

Para problemas de instalación, incluidos el alta y los certificados, consulta
[instalacion.md](instalacion.md). Para saber qué puede hacer el panel como root y qué se graba, consulta
[seguridad.md](seguridad.md).
