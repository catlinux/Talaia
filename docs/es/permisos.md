# Usuarios, permisos y claves de API

[Read in English](../en/permissions.md) · [Volver al README](../../README.es.md)

Esta guía explica quién puede hacer qué en Talaia, hasta dónde llega una asignación y cómo encajan
las claves de API, el inicio de sesión único, las invitaciones y las grabaciones de terminal. El
diseño está en [arquitectura.md](arquitectura.md); el modelo de seguridad, en [seguridad.md](seguridad.md).

## El modelo en lenguaje llano

Hay **una sola organización**. Dentro de ella:

- Los **grupos de servidores** agrupan servidores (por ejemplo `produccion` o `clientes`).
- Los **equipos** agrupan usuarios para dar el mismo rol a varias personas a la vez.
- Una **asignación de rol** da un rol fijo a un usuario, un equipo o una clave de API sobre un ámbito.

Los ámbitos forman un árbol. Una asignación se aplica a su nodo y a todo lo que cuelga de él:

```text
Organización
└── Grupo
    └── Servidor
        └── Stack
            └── Contenedor
```

Un stack o un contenedor se identifican por su servidor y su nombre. Un contenedor que pertenece a un
proyecto Compose también hereda las asignaciones de ese stack; un contenedor suelto hereda solo del
servidor y de los niveles superiores.

## Los cinco roles

Estos son los únicos roles. Los roles personalizados no forman parte de esta versión.

| Rol                                          | Qué puede hacer                                                                                                                                                                                                                                                                 | Qué no puede hacer                                                                                                                                                                               |
| -------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **Administrador** (`admin`)                  | Todo lo que permita el ámbito: usuarios, ajustes, servidores, stacks, contenedores, operaciones del host, auditoría y grabaciones                                                                                                                                               | Nada dentro de Talaia, pero equivale a root en los servidores gestionados (ver abajo)                                                                                                            |
| **Operador de servidor** (`server_operator`) | Contenedores (ver, registros, iniciar/parar/reiniciar, terminal), recursos de Docker, stacks (ver, desplegar, editar, borrar contenedores) y el servidor entero: servicios, journal, apt, reinicio, terminal del anfitrión, actualizaciones del agente                          | Gestionar usuarios, equipos, grupos, invitaciones o claves de API; cambiar ajustes u OIDC; quitar o dar de alta servidores; leer la auditoría o las grabaciones                                  |
| **Operador de app** (`app_operator`)         | Ver contenedores, stacks y servidores; con ámbito de servidor, leer servicios, estado de apt y journal; con ámbito de organización, leer auditoría y grabaciones; leer registros de contenedores; iniciar/parar/reiniciar contenedores; desplegar stacks (up/down/restart/pull) | Editar o adoptar un archivo Compose; abrir una terminal de contenedor o del anfitrión; cambiar servicios del host, apt o reinicio; quitar o dar de alta servidores; gestionar usuarios o ajustes |
| **Lector** (`viewer`)                        | Leer servidores, stacks, contenedores y registros; cuando el ámbito llega al servidor, leer servicios, estado de apt y journal; con ámbito de organización, leer la auditoría y las grabaciones                                                                                 | Cambiar contenedores o stacks, abrir terminales, editar compose, ejecutar acciones del host, quitar o dar de alta servidores, gestionar usuarios o ajustes                                       |
| **Solo métricas** (`metrics_only`)           | Estado y métricas de servidores, stacks y contenedores                                                                                                                                                                                                                          | Registros, servicios, journal, apt, terminales, auditoría, grabaciones, cualquier cambio y cualquier administración                                                                              |

Los permisos del rol se filtran por el ámbito de la asignación. Por ejemplo, un administrador asignado
solo a un stack recibe la parte del rol de administrador que corresponde al stack, no usuarios ni
acceso al host.

## La regla del ámbito mínimo

Un permiso se concede solo cuando **el ámbito de la asignación es al menos tan amplio como el ámbito
mínimo del permiso**:

| Ámbito mínimo | Permisos                                                                                                                                                                                     |
| ------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Contenedor    | `container.view`, `container.logs`, `container.control`, `container.terminal`                                                                                                                |
| Stack         | `stack.view`, `stack.deploy`, `stack.edit`, `container.remove`                                                                                                                               |
| Servidor      | `server.view`, `resources.manage`, `host.services.view`, `host.services.control`, `host.journal`, `host.apt.view`, `host.apt.upgrade`, `host.reboot`, `host.terminal`, `server.agent_update` |
| Organización  | `server.remove`, `server.enroll`, `users.manage`, `settings.manage`, `audit.view`, `recordings.view`, `apikeys.manage`                                                                       |

Un grupo no tiene permisos propios; transmite las asignaciones a sus servidores. Una asignación de
organización llega a todo.

La regla evita la escalada de privilegios. Un operador de servidor asignado a un stack puede abrir una
terminal de contenedor **dentro de ese stack**, pero no ve los servicios del host, no reinicia el
servidor ni edita otro stack: los permisos del host necesitan una asignación de servidor, y los de
organización necesitan una asignación de organización.

Esto no hace inofensiva una asignación de stack: `stack.edit` puede ejecutar código arbitrario como root
a través del archivo Compose. Da el rol de operador de servidor, o un permiso `stack.edit`, solo a quien
confiarías root en ese servidor. El rol de operador de app carece de él a propósito.

## Por qué un operador de app no puede editar el Compose ni abrir una terminal

Un archivo Compose se ejecuta como root en el servidor. Quien pueda editarlo puede montar `/`, arrancar
un contenedor privilegiado, usar la red o el espacio de PID del host o añadir capacidades. Editar
Compose equivale, por tanto, a root en el host, no es una operación de la aplicación.

Lo mismo vale para las terminales: un shell de root, o un shell en un contenedor privilegiado, puede
cambiar el host. El rol de operador de app no tiene ni `stack.edit` ni `container.terminal` ni
`host.terminal`. Puede desplegar los archivos que ya existen, pero no cambiarlos.

El editor de stacks también muestra una nota permanente sobre esto. Véase [seguridad.md](seguridad.md).

## Dar acceso a una sola app

1. Entra como administrador y abre **Usuarios**.
2. Elige **Acceso** en un usuario existente, o crea una **Invitar usuario** con el mismo acceso.
3. Selecciona el rol **Operador de aplicaciones**.
4. Pon el ámbito en **Stack**, elige el servidor y escribe el nombre del stack (por ejemplo `shop`).
5. Guarda. La persona ya puede ver ese stack, desplegarlo y leer sus registros, pero no editar su
   archivo Compose, abrir una terminal ni llegar al resto del servidor.

Una invitación también necesita un nombre de usuario; el correo es opcional. La persona invitada
recibe el enlace de tu parte; Talaia no envía correo.

## 404, 403 y el registro de auditoría

El núcleo decide, no el panel. El panel solo esconde los controles que el usuario no puede usar.

- **404 Not Found** significa que el usuario no puede ver el objeto. El panel no revela que exista un
  servidor, stack o contenedor.
- **403 Forbidden** significa que puede ver el objeto, pero el rol no permite esa operación.
- Una ruta sin permiso declarado está cerrada por defecto y la prueba guardiana falla.
- Cada denegación se escribe en la auditoría como `authz_denied`, con el actor, el permiso, el método,
  la ruta, el resultado y el ámbito (y el id de la clave de API cuando se usó una). Una petición
  denegada nunca revela el objeto ni el motivo a quien la hace.

## El último administrador

Talaia mantiene siempre al menos un usuario activo con el rol de administrador de la organización,
directo o a través de un equipo. El núcleo se niega a desactivar al último, o a quitar o degradar la
última asignación de administrador de organización (también a través de un equipo), con
`409 last_admin`. En esta versión no hay endpoint para borrar usuarios. Los usuarios tampoco pueden
desactivar su propia cuenta.

## Invitaciones

- Un administrador crea una invitación con un rol y un ámbito iniciales.
- El enlace tiene la forma `https://panel.example.com:8443/invite#<token>`. El token se muestra una vez
  y se guarda con hash.
- Caduca a las **72 horas** y sirve **una sola vez**.
- La persona invitada elige una contraseña (12–256 caracteres) y debe configurar TOTP antes de tener
  una sesión completa. El segundo factor es obligatorio para todos.
- Talaia no envía correo; comparte el enlace por un canal de confianza.

## Claves de API

Las claves de API son para scripts y CI. Tienen sus propias asignaciones de rol y siempre están
limitadas por los permisos actuales de su dueño.

- **Crear:** **Ajustes > Claves de API**, elige un nombre, duración y acceso, y escribe un código TOTP
  reciente.
- **Formato:** `tlk_<id de clave de 12 caracteres>_<secreto de 32 caracteres>`.
- **Se muestra una vez:** solo se guarda el hash del secreto. Cópiala cuando aparezca el diálogo; no se
  puede recuperar.
- **Duración:** 90 días por defecto; 365 días como máximo.
- **Intersección:** una clave solo puede hacer lo que permiten a la vez sus propias asignaciones y las
  asignaciones actuales de su dueño. Si el dueño pierde acceso, la clave también. Desactivar al dueño
  revoca sus claves.
- **No puede:** abrir terminales de contenedor o del anfitrión, gestionar usuarios, equipos, grupos,
  claves ni ajustes, reiniciar un servidor ni autenticarse en los endpoints de inicio de sesión.
- **Límite:** 600 peticiones por minuto por clave, con un límite adicional por IP.
- **Revocar:** desde la misma sección, con confirmación. La revocación es inmediata.
- **Último uso:** la lista muestra cuándo se usó la clave por última vez.

Crea una clave en el panel y úsala en una cabecera. El panel muestra este ejemplo:

```bash
curl -H "Authorization: Bearer $TALAIA_API_KEY" \
  https://panel.example.com:8443/api/v1/servers
```

Una clave no necesita cookie, token CSRF ni cabecera `Origin`. No la pongas en una URL ni en una orden
de shell que puedan leer otros usuarios.

## Inicio de sesión único (OIDC)

Talaia admite un proveedor genérico de OpenID Connect.

1. En **Ajustes > Inicio de sesión único**, escribe la URL del emisor, el ID de cliente, el secreto de
   cliente y el texto del botón.
2. Registra en el proveedor la URL de redirección que muestra el panel. Normalmente es
   `https://panel.example.com:8443/api/v1/auth/oidc/callback`; usa el valor exacto que aparezca.
3. Activa y guarda. Al guardar se comprueba el descubrimiento; un emisor incorrecto o un proveedor
   inalcanzable devuelve `422 oidc_discovery_failed` y la configuración no se activa.
4. La página de acceso muestra **Inicia sesión con <texto>** cuando la configuración está activa.

El secreto de cliente es de solo escritura: el núcleo lo guarda cifrado y nunca lo devuelve. Si dejas
el campo vacío, se conserva el secreto guardado.

OIDC sustituye **solo la contraseña**:

- El correo del proveedor debe estar verificado y coincidir con un usuario activo existente. No hay
  alta automática de cuentas.
- En el primer acceso correcto se vincula el `sub` del proveedor a ese usuario. A partir de entonces
  manda el `sub`; un subject distinto para el mismo correo se rechaza.
- El segundo factor (TOTP o clave de acceso) sigue siendo obligatorio.
- Los fallos vuelven al acceso con un mensaje genérico y se auditan como `oidc_login_denied`.

Usa HTTPS para el emisor, salvo `http://localhost` o `http://127.0.0.1` en desarrollo.

## Grabaciones de terminal

Quien tenga `recordings.view` (ámbito de organización) encuentra **Auditoría > Grabaciones**. La tabla
muestra cuándo, quién, servidor, contenedor o anfitrión, duración, motivo de cierre e insignia de
privilegiada. Las sesiones terminadas tienen un enlace **Descargar .cast**.

```bash
asciinema play sesion.cast
```

Las claves de API nunca pueden leer ni descargar grabaciones. Las descargas se auditan. El panel no
tiene reproductor integrado. Las grabaciones pueden contener secretos impresos en pantalla; véase
[seguridad.md](seguridad.md).

## talaiactl

`talaiactl` usa una clave de API y la misma API. Las órdenes actuales son:

```text
login
servers list
containers list <servidor>
containers start|stop|restart <servidor> <contenedor>
stacks list [servidor]
stacks up|down|restart|pull <servidor> <stack>
jobs wait <id>
version
help
```

`<servidor>` acepta el ID del servidor o su nombre exacto. Si más de un servidor tiene ese nombre, la
orden se detiene y pide el ID.

`talaiactl login` pide la URL si falta, lee la clave sin eco cuando la entrada es un terminal (si no, de
una línea), la verifica con `GET /api/v1/me`, imprime el nombre de usuario y guarda la configuración.

La configuración se lee de:

- Linux: `~/.config/talaia/config.toml` (el directorio que devuelve `os.UserConfigDir()`).
- Windows: `%AppData%\talaia\config.toml`.
- El archivo contiene `url` y `api_key` y se crea con modo `0600`. En Unix, talaiactl se niega a leerlo
  si el grupo u otros usuarios pueden leerlo.

```toml
url = "https://panel.example.com:8443"
api_key = "tlk_..."
```

Las variables de entorno y los parámetros sustituyen al archivo:

| Ajuste       | Entorno                           | Parámetro                                                    |
| ------------ | --------------------------------- | ------------------------------------------------------------ |
| URL          | `TALAIA_URL`                      | `--url`                                                      |
| Clave de API | `TALAIA_API_KEY`                  | `--api-key` (desaconsejado: aparece en la lista de procesos) |
| CA adicional | `TALAIA_CA_FILE`                  | `--ca-file`                                                  |
| Idioma       | `TALAIA_LANG` / `LC_ALL` / `LANG` | `--lang ca\|es\|en`                                          |

Ejemplos:

```bash
talaiactl login
talaiactl servers list
talaiactl containers list vps-web-01
talaiactl containers restart vps-web-01 web
talaiactl stacks list
talaiactl stacks up vps-web-01 shop
talaiactl containers restart vps-web-01 web --wait --timeout 5m
talaiactl jobs wait <job-id>
talaiactl --json servers list
```

Las acciones imprimen el id de la tarea. Con `--wait` siguen la tarea; `jobs wait` consulta cada 2
segundos hasta `--timeout` (10 minutos por defecto), imprime la salida y termina con código distinto de
cero si la tarea falló. La salida por defecto es una tabla alineada; `--json` imprime los datos crudos
de la API como JSON.

TLS usa las raíces del sistema más `--ca-file` o `TALAIA_CA_FILE`; no hay opción para saltarse la
verificación. El cliente envía `User-Agent: talaiactl/<versión>`. Los códigos de salida son 0 para
éxito, 1 para error y 2 para uso incorrecto.

## Documentación relacionada

- [Arquitectura](arquitectura.md) — modelo de datos y puntos de aplicación.
- [Seguridad](seguridad.md) — modelo de amenazas, grabaciones y qué se audita.
- [Acceso](acceso.md) — contraseña, TOTP, dispositivos de confianza y claves de acceso.
