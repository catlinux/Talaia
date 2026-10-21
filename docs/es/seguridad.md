# Seguridad

[Read in English](../en/security.md) · [Volver al README](../../README.es.md)

Qué puede hacer el panel con tus servidores, qué lo protege y qué se graba. El diseño está en
[arquitectura.md](arquitectura.md); los pasos de instalación, en [instalacion.md](instalacion.md).

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
- **Las actualizaciones desde el panel** las aplica un ayudante con privilegios de root en la máquina del núcleo
  ([instalacion.md](instalacion.md), sección 5). El núcleo solo puede pedir un commit de `origin/main` más nuevo
  que el que corre.

Cuida esa cuenta: una contraseña larga y única, una aplicación de autenticación que controles y no compartirla.
Los roles y abasto por usuario (por ejemplo, acceso a un solo stack) llegan en una fase posterior; cuando lleguen,
los permisos de editar stacks, adoptar proyectos y abrir terminales se marcarán como equivalentes a root y no se
concederán por defecto.

## Qué protege el panel y los agentes

- **Acceso:** contraseña y TOTP, límite de intentos, bloqueo tras fallos repetidos, sesiones que caducan y
  protección de cada cambio frente a peticiones de otros sitios (comprobación del origen).
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
  contenido de ficheros, texto de registros ni lo que se escribe en una terminal.

## Sesiones de terminal: grabación y retención

Una sesión de terminal siempre se graba, y no se abre si no se puede grabar.

- **Qué se graba:** la *salida* de la terminal y sus cambios de tamaño, en formato asciicast v2. **Lo que
  escribes no se graba**, pero lo que el shell devuelve con eco forma parte de la salida: una contraseña
  escrita en un prompt que la muestra, o impresa por un comando, queda en la grabación. La interfaz lo recuerda
  de forma permanente.
- **Dónde:** en la base de datos del núcleo, en trozos que se cifran uno a uno con la clave de `secrets.key`
  (volumen `core-data`). Sin ese fichero las grabaciones no se pueden leer, y lo mismo vale para una copia de la
  base de datos sola.
- **Retención:** 90 días por defecto. Se cambia con `TALAIA_TERMINAL_RECORDING_DAYS` (o `terminal_recording_days`
  en el fichero de configuración). Las más antiguas se borran al arrancar el núcleo y cada 24 horas.
- **Límites:** como máximo 4 sesiones por servidor y una por usuario y contenedor; una sesión se cierra tras 15
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
cierre, usuario, contenedor, duración y bytes grabados, más `root-equivalent` en los contenedores privilegiados.
`terminal export` descifra una grabación y escribe el fichero `.cast` por la salida estándar.

## Informar de una vulnerabilidad

No abras un issue público: sigue [SECURITY.es.md](../../SECURITY.es.md).
