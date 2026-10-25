# Monitorización, alertas y comprobaciones

[Read in English](../en/monitoring.md) · [Volver al README](../../README.es.md)

Esta guía explica qué mide Talaia, cómo guarda y dibuja las métricas, cómo funcionan las alertas y las
comprobaciones, adónde van las notificaciones y qué tráfico saliente se bloquea. Es una función de la fase 5,
todavía en curso. La referencia técnica es [arquitectura.md](arquitectura.md); los permisos están en
[permisos.md](permisos.md) y las reglas de salida y secretos en [seguridad.md](seguridad.md).

## Contenido

[Registro](#qué-se-registra) · [Resolución y retención](#resoluciones-reducción-y-retención) · [Gráficos](#gráficos-del-panel) · [Alertas](#reglas-de-alerta) · [Evaluación](#cómo-se-evalúan-las-alertas) · [Contenedores](#vigilancia-de-contenedores-y-reinicios) · [Silencios](#silencios) · [Comprobaciones](#comprobaciones) · [Canales](#canales-de-notificación) · [Salida](#protección-del-tráfico-saliente) · [Permisos](#quién-puede-hacer-qué) · [API](#endpoints-de-la-api) · [Prueba](#probarlo-paso-a-paso) · [Problemas](#solución-de-problemas)

## Qué se registra

El agente envía una muestra del anfitrión cada **15 segundos**:

| Serie | Significado |
|---|---|
| CPU | Uso de CPU de toda la máquina, 0–100 % |
| Memoria | Bytes usados y totales |
| Disco | Bytes usados y totales del sistema de archivos raíz (`/` en Linux, la unidad del sistema en Windows) |
| Red | Bytes recibidos y enviados por las interfaces físicas |

El núcleo convierte los contadores de red en **tasas de bytes por segundo** comparando dos muestras
consecutivas; la primera muestra no tiene tasa. Las interfaces de bucle local, Docker, puente, veth y de
máquinas virtuales quedan fuera de la serie de red del anfitrión.

El agente también muestrea cada **contenedor en ejecución** cada 15 segundos: porcentaje de CPU y memoria
usada/límite. La memoria del contenedor resta la caché de páginas que Docker informa como inactiva, así que
se acerca más al conjunto de trabajo real. El agente envía su inventario cada **60 segundos**, justo después
de un evento de Docker, y un latido cada 10 segundos. No guarda un historial local de métricas; lo guarda el
núcleo.

## Resoluciones, reducción y retención

Las muestras se guardan en tres resoluciones:

| Resolución | Origen | Se usa para |
|---|---|---|
| `raw` | Una fila por muestra del agente | Rangos de hasta 6 horas |
| `5m` | Media y máximo de cada bloque de 5 minutos | Rangos de hasta 7 días |
| `1h` | Media y máximo de cada bloque de 1 hora, a partir de `5m` | Rangos de más de 7 días |

La reducción se ejecuta cada **5 minutos**. Un bloque solo se escribe cuando está completo. CPU, memoria y
disco guardan media y máximo; la red guarda la media. Las tablas son hypertables de TimescaleDB y los bloques
antiguos se eliminan aproximadamente un minuto después de arrancar y luego cada 24 horas.

Los valores por defecto son:

| Dato | Bruto | 5 minutos | 1 hora |
|---|---:|---:|---:|
| Métricas del servidor | 48 horas | 30 días | 400 días |
| Métricas de contenedores | 24 horas | 7 días | 90 días |

Se pueden cambiar en el archivo TOML del núcleo (`/etc/talaia/core.toml`, o el que indique `TALAIA_CONFIG`).
La pila Compose empaquetada pasa la configuración por variables de entorno y no monta un archivo TOML, así
que estos valores por defecto se aplican salvo que aportes uno.

```toml
[metrics]
server_raw_hours = 48
server_5m_days = 30
server_1h_days = 400
container_raw_hours = 24
container_5m_days = 7
container_1h_days = 90
```

Las horas aceptan 1–87600 y los días 1–3650. Los valores se leen al arrancar el núcleo.

## Gráficos del panel

Abre un servidor y baja hasta **Historial**. Los cuatro gráficos muestran CPU, memoria, disco y red. El
selector de rango ofrece **1 hora, 6 horas, 24 horas, 7 días y 30 días**; el panel refresca los datos cada
30 segundos. Memoria y disco se dibujan como porcentaje de sus totales.

## Reglas de alerta

Talaia incluye seis reglas integradas:

| Regla | Tipo | Gravedad | Comportamiento por defecto |
|---|---|---|---|
| Servidor sin conexión | `server_offline` | Crítica | El agente está desconectado y su último contacto fue hace al menos 45 segundos |
| Uso de disco alto | `disk_high` | Crítica | El sistema de archivos raíz está al 90 % o más |
| Contenedor parado | `container_down` | Crítica | Un contenedor vigilado lleva 20 segundos sin ejecutarse |
| Contenedor no saludable | `container_unhealthy` | Aviso | La comprobación de salud de un contenedor lleva 60 segundos sin estar sana |
| Comprobación fallida | `check_failed` | Crítica | Una comprobación falla dos veces seguidas |
| Certificado a punto de caducar | `cert_expiry` | Aviso | El certificado de una comprobación caduca en 14 días o menos |

Las reglas integradas se pueden editar y desactivar, pero no eliminar. Talaia no crea reglas de CPU ni de
memoria por defecto; añádelas en **Ajustes > Alertas > Reglas** si las necesitas.

Una regla tiene un tipo, una gravedad (`warning` o `critical`), un ámbito, parámetros, un intervalo de
repetición opcional y una lista de canales. El ámbito puede ser **organización**, **grupo**, **servidor**,
**stack** o **contenedor**; el panel usa los selectores de servidor y grupo existentes y pide el nombre del
stack o contenedor cuando hace falta. Si la lista de canales está vacía, la regla usa **todos los canales
activos**.

Estos son los tipos de regla y sus parámetros (los mismos rangos que valida el núcleo):

| Tipo | Parámetros | Rango |
|---|---|---|
| `server_offline` | `seconds` | 0–86400 |
| `cpu_high` | `threshold` (%), `for_seconds` | 1–100, 0–86400 |
| `mem_high` | `threshold` (%), `for_seconds` | 1–100, 0–86400 |
| `disk_high` | `threshold` (%), `for_seconds` | 1–100, 0–86400 |
| `container_down` | `seconds` | 0–86400 |
| `container_unhealthy` | `seconds` | 0–86400 |
| `check_failed` | `consecutive_failures` | 1–20 |
| `cert_expiry` | `days` | 1–365 |

El intervalo de repetición va de 0 a 604800 segundos. El panel ofrece **nunca, 5 minutos, 15 minutos,
1 hora, 6 horas y 24 horas**. El valor 0 significa que la alerta se notifica una vez al activarse y otra al
resolverse.

## Cómo se evalúan las alertas

El motor de alertas evalúa cada **5 segundos**, y de inmediato cuando cambia el inventario de un servidor o
termina una comprobación. Su estado sale de la última instantánea del agente y de los últimos resultados de
las comprobaciones.

- **Duración pendiente.** Las reglas de CPU, memoria, disco, contenedor y servidor sin conexión pueden exigir
  que la condición se mantenga un tiempo. El contador empieza en la primera evaluación fallida; la alerta se
  abre solo cuando ha pasado la duración completa.
- **Histéresis.** Una vez activa una alerta de CPU, memoria o disco, sigue activa hasta que el valor cae al
  menos **2 puntos por debajo** del umbral. Una regla del 90 % se despeja por debajo del 88 %.
- **Enfriamiento.** Si la misma regla y el mismo sujeto vuelven a activarse en los **10 minutos** siguientes
  a la última notificación, Talaia registra el incidente pero suprime la nueva notificación de activación.
  Así se evita que un estado intermitente inunde un canal.
- **Repetición.** Mientras una alerta está activa, no está confirmada y tiene intervalo de repetición,
  Talaia envía otra notificación de activación cuando se cumple el intervalo.
- **Confirmación.** Confirmar detiene las notificaciones repetidas y añade la marca «Confirmada por…». No
  resuelve el incidente; Talaia lo resuelve cuando la condición desaparece.
- **Margen de arranque.** Las reglas `server_offline` no se evalúan durante los primeros **90 segundos**
  después de arrancar el núcleo. Los demás tipos pueden activarse de inmediato.
- **Incidentes silenciados.** Un silencio suprime el envío, no la detección: la alerta se abre y se guarda
  igualmente, y el panel la marca como silenciada.

## Vigilancia de contenedores y reinicios

Talaia no vigila todos los nombres de contenedor que ha visto alguna vez. Vigila un contenedor solo después
de haberlo observado **en ejecución**. Un contenedor parado, tumbado o eliminado **desde Talaia** deja de
vigilarse a propósito, para que la acción solicitada no genere una alerta:

- parar o eliminar un contenedor en el panel;
- tumbar, eliminar u olvidar un stack.

Un contenedor parado fuera de Talaia sigue vigilado y genera `container_down` tras los segundos configurados.
Si vuelve a arrancar, la alerta se resuelve y la vigilancia continúa.

Un **reinicio de servidor iniciado desde Talaia** crea un silencio de servidor de **10 minutos** con motivo
`reboot`. El silencio suprime las alertas
mientras la máquina se reinicia; se ve en la pestaña Silencios.

### ¿Con cuánta rapidez se detecta un contenedor parado?

Los eventos de Docker despiertan al agente de inmediato. El agente agrupa los inventarios provocados por
eventos durante **2 segundos** y envía uno; el núcleo evalúa ese servidor en cuanto llega, además de su
temporizador de 5 segundos. Una regla `container_down` por defecto espera 20 segundos, y el trabajador de
notificaciones consulta la cola de envíos cada **5 segundos**. Por tanto, un contenedor parado con un canal
que funcione se suele avisar en menos de un minuto. Un disco que supera el 90 % no tiene espera pendiente, así
que se avisa tras la siguiente muestra y evaluación, también normalmente en menos de un minuto.

## Silencios

Un silencio suprime las notificaciones de un ámbito y, opcionalmente, de un tipo de regla. Se puede crear
desde una alerta; la pestaña Silencios los lista y permite borrarlos. La API aplica estos límites:

- **30 días como máximo**; el panel ofrece 30 minutos, 1 hora, 4 horas, 24 horas, 7 días y un número
  personalizado de minutos hasta 43200;
- **motivo obligatorio**, de hasta 200 caracteres;
- ámbito **organización, grupo, servidor, stack o contenedor**.

Talaia sigue registrando el incidente y lo conserva en el historial. Solo se suprime el envío. Un silencio no
confirma una alerta. Borrar un silencio no envía lo que se suprimió mientras duró; las repeticiones posteriores y la resolución del incidente se envían con normalidad.

## Comprobaciones

Las comprobaciones se ejecutan desde el núcleo, no desde el agente. La **asociación opcional a un servidor**
vincula las alertas de la comprobación a ese servidor y su ámbito; no ejecuta la sonda en el agente.

| Tipo | Objetivo | Qué hace |
|---|---|---|
| HTTP | URL completa `http://` o `https://` | Envía `GET` o `HEAD`, sigue hasta tres redirecciones, lee como máximo 64 KiB del cuerpo y compara el estado |
| TCP | `host:puerto` | Abre una conexión TCP protegida |
| TLS | `host:puerto` | Abre TCP, hace un saludo TLS 1.2 o superior y valida el certificado |

Las opciones HTTP son el método (`GET` o `HEAD`), los códigos esperados (códigos sueltos o rangos separados
por comas, por ejemplo `200,301-399`) y un tiempo de espera de **1–10 segundos**. Por defecto: `GET`,
`200-399` y 10 segundos. La protección de salida vuelve a comprobar cada redirección. Un bucle o más de tres
redirecciones hacen fallar la comprobación.

El intervalo mínimo es de **30 segundos** y el máximo de 24 horas. Talaia guarda la hora, el éxito o fallo, la
latencia en milisegundos, el código de estado HTTP (en HTTP), un error corto y la fecha de caducidad del
certificado (cuando la respuesta o el saludo la expusieron). **Nunca guarda el cuerpo de la respuesta HTTP**,
credenciales de la petición ni una copia del contenido del destino. Los resultados de más de 30 días se
borran; la limpieza se ejecuta al arrancar y después una vez al día.

Una comprobación se puede desactivar, editar y eliminar. **Ejecutar ahora** la lanza de inmediato, guarda el
resultado y lo muestra en el panel. La regla `cert_expiry` evalúa el valor `cert_not_after`; no hace un
escaneo aparte.

## Canales de notificación

Configura los canales en **Ajustes > Alertas > Canales**. Cada canal tiene un nombre, un tipo, un interruptor
de activación y un idioma (`ca`, `es` o `en`) que controla el texto del mensaje. Se puede probar en el sitio:
la prueba se envía de inmediato y se muestra el éxito o el error censurado.

| Tipo | Configuración | Secreto |
|---|---|---|
| Telegram | ID del chat | Token del bot |
| ntfy | URL del servidor (por defecto `https://ntfy.sh`), prioridad 1–5 | Tema; token de acceso opcional |
| Correo | Host SMTP, puerto, dirección remitente, lista de destinatarios, STARTTLS o TLS implícito | Usuario y contraseña |
| Discord | Nada más | URL del webhook de Discord |
| Webhook | Nada más | URL del destino; secreto HMAC opcional |

El correo **nunca envía texto plano**: solo acepta STARTTLS y TLS implícito. Discord solo acepta una URL
oficial de `discord.com` o `discordapp.com` bajo `/api/webhooks/`. ntfy envía el título, la prioridad y las
etiquetas de gravedad, y usa un token Bearer cuando está configurado.

Las notificaciones de activación y resolución se encolan. Un envío fallido se reintenta a los **30 segundos,
2 minutos, 5 minutos y 15 minutos**, hasta un máximo de cinco intentos. El trabajador consulta la cola cada
5 segundos. La columna `último estado` muestra la última fecha correcta o el último error, y el error se
censura antes de guardarlo o mostrarlo.

### Firma del webhook

Un webhook envía un objeto JSON con `version`, `event`, `alert` y `sent_at`. Si hay un secreto HMAC
configurado, cada petición lleva:

- `X-Talaia-Timestamp`: segundos Unix;
- `X-Talaia-Signature`: `sha256=<HMAC-SHA256 en hexadecimal>`.

Los bytes firmados son la marca de tiempo, un punto literal y el **cuerpo exacto de la petición**:

```text
HMAC-SHA256(secreto, timestamp + "." + cuerpo)
```

Verifícalo antes de interpretar o confiar en el JSON. Este ejemplo de Python usa la biblioteca estándar:

```python
import hashlib
import hmac

secret = b"replace-with-the-channel-secret"
timestamp = headers["X-Talaia-Timestamp"]
body = raw_request_body  # bytes exactos leídos de la petición
expected = hmac.new(
    secret,
    timestamp.encode("ascii") + b"." + body,
    hashlib.sha256,
).hexdigest()
received = headers["X-Talaia-Signature"].removeprefix("sha256=")
if not hmac.compare_digest(expected, received):
    raise SystemExit("invalid signature")
```

No vuelvas a serializar el JSON antes de comprobar la firma: un cambio de espacios o de orden de claves la
invalidaría. La cabecera de firma no aparece si el canal no tiene secreto HMAC.

### Secretos

La API devuelve `has_secret`, nunca el secreto. En modo edición el panel muestra **Secreto guardado** y una
acción **Sustituir**; los campos siguen vacíos hasta que se sustituyen. Guardar sin sustituir no envía ningún
campo de secreto, así que se conserva el guardado. Guardar con sustitución envía solo el valor nuevo. Los
campos secretos usan `type="password"` y `autocomplete="new-password"`.

Los secretos de los canales se cifran con AES-256-GCM usando la misma `data_dir/secrets.key` que el TOTP y el
secreto de cliente de OIDC. Los errores se censuran contra cada valor secreto antes de guardarse en el estado
del canal o devolverse al panel.

## Protección del tráfico saliente

Las comprobaciones y los canales pueden hacer que el núcleo se conecte a URL arbitrarias. Talaia valida el
destino **después de resolver el DNS, en el momento de conectar**, así que una respuesta de DNS rebinding no
puede saltarse la política. Las redirecciones HTTP y cada dirección resuelta se comprueban de nuevo. Las
credenciales en la URL se rechazan y para objetivos HTTP solo se aceptan `http` y `https`; las variantes
numéricas no canónicas de IPv4 también se rechazan.

Por defecto se bloquean:

- direcciones de bucle local, privadas RFC 1918, NAT de operador (`100.64.0.0/10`), link-local y únicas
  locales de IPv6;
- rangos de pruebas, documentación y reservados;
- direcciones multicast, no especificadas y de difusión;
- direcciones de metadatos de nube.

El interruptor **Permitir que las comprobaciones y las notificaciones lleguen a redes privadas** de
**Ajustes > Alertas** relaja las restricciones de direcciones privadas, de bucle local y NAT de operador. **No**
relaja las link-local, los rangos reservados de documentación, multicast/difusión ni los metadatos de nube.
`169.254.169.254` y `fd00:ec2::254` siguen bloqueadas diga lo que diga el interruptor, porque pueden exponer
credenciales de instancia y datos de control del proveedor. Déjalo apagado salvo que
el núcleo y los destinos estén en una red de confianza.

## Quién puede hacer qué

| Rol | Capacidad de alertas |
|---|---|
| Administrador (`admin`) | Ver, confirmar/silenciar y gestionar reglas, comprobaciones, canales y ajustes |
| Operador de servidor (`server_operator`) | Ver y silenciar alertas en su ámbito |
| Operador de app (`app_operator`) | Ver y silenciar alertas en su ámbito |
| Lector (`viewer`) | Ver alertas en su ámbito |
| Solo métricas (`metrics_only`) | Ver alertas en su ámbito |

Los permisos son:

| Permiso | Ámbito mínimo | Permite |
|---|---|---|
| `alerts.view` | Contenedor | Ver alertas, historial, resúmenes y silencios del ámbito |
| `alerts.silence` | Stack | Confirmar, crear/borrar silencios y dejar de vigilar un contenedor |
| `alerts.manage` | Organización | Reglas, comprobaciones, canales, envíos de prueba y el ajuste de redes privadas |

Solo `admin` tiene `alerts.manage`; la sección **Ajustes > Alertas** se oculta a todos los demás. Las claves
de API pueden usar `alerts.view` y `alerts.silence`, pero nunca `alerts.manage`: las rutas de gestión rechazan
las claves de API aunque sus asignaciones de rol las permitieran.

## Endpoints de la API

Todas las rutas están bajo `/api/v1`. Las respuestas son JSON; las escrituras requieren sesión, un `Origin`
permitido y las reglas CSRF habituales salvo que se acepte una clave de API.

| Método y ruta | Para qué | Permiso |
|---|---|---|
| `GET /servers/{id}/metrics` | Historial del servidor; `range=1h\|6h\|24h\|7d\|30d`, o `from`/`to` | `server.view` |
| `GET /servers/{id}/containers/{name}/metrics` | Historial del contenedor; los mismos rangos | `container.view` |
| `GET /alerts` | Lista activa o historial; `state`, `limit`, `offset`, `server_id` | `alerts.view` en cada destino |
| `GET /alerts/summary` | Recuentos y gravedad máxima por servidor | `alerts.view` en cada destino |
| `POST /alerts/{id}/ack` | Confirmar | `alerts.silence` |
| `POST /alerts/{id}/unwatch` | Dejar de vigilar una alerta de contenedor | `alerts.silence` |
| `GET /silences` | Silencios activos | `alerts.view` |
| `POST /silences` | Crear un silencio | `alerts.silence` |
| `DELETE /silences/{id}` | Borrar un silencio | `alerts.silence` |
| `GET /alert-rules` | Listar reglas | `alerts.view` en organización |
| `POST /alert-rules` | Crear una regla | `alerts.manage` en organización |
| `PATCH /alert-rules/{id}` | Editar o desactivar una regla | `alerts.manage` en organización |
| `DELETE /alert-rules/{id}` | Borrar una regla no integrada | `alerts.manage` en organización |
| `GET /checks` | Listar comprobaciones y su último resultado | `alerts.view` en organización |
| `POST /checks` | Crear una comprobación | `alerts.manage` en organización |
| `PATCH /checks/{id}` | Editar una comprobación | `alerts.manage` en organización |
| `DELETE /checks/{id}` | Borrar una comprobación | `alerts.manage` en organización |
| `POST /checks/{id}/run` | Ejecutar ahora y guardar el resultado | `alerts.manage` en organización |
| `GET /channels` | Listar canales sin secretos | `alerts.manage` en organización |
| `POST /channels` | Crear un canal | `alerts.manage` en organización |
| `PATCH /channels/{id}` | Editar un canal; omite `secret` para conservarlo | `alerts.manage` en organización |
| `DELETE /channels/{id}` | Borrar un canal | `alerts.manage` en organización |
| `POST /channels/{id}/test` | Enviar un mensaje de prueba | `alerts.manage` en organización |
| `GET /settings/alerts` | Leer `allow_private_targets` | `alerts.manage` en organización |
| `PUT /settings/alerts` | Cambiar `allow_private_targets` | `alerts.manage` en organización |

Las escrituras de reglas, comprobaciones, canales y ajustes están marcadas **sin clave de API**: una clave no
puede usarlas aunque tenga `alerts.manage`.

## Probarlo paso a paso

Usa un servidor de pruebas y un canal cuyo botón de prueba ya funcione.

### Parar un contenedor desechable

1. Crea un contenedor que no hace nada:

   ```bash
   docker run -d --name talaia-alert-test busybox sleep 3600
   ```

2. Espera unos 10 segundos a que Talaia lo vea en ejecución y registre la vigilancia.
3. Páralo **desde la shell del servidor**, no desde el panel de Talaia, porque una parada pedida en el panel
   deja de vigilarse a propósito:

   ```bash
   docker stop talaia-alert-test
   ```

4. Con la regla `container_down` por defecto (20 segundos), la alerta y su notificación deberían llegar en
   menos de un minuto. Revisa **Alertas > Activas** y el último estado del canal.
5. Arráncalo de nuevo para resolver la alerta y elimínalo:

   ```bash
   docker start talaia-alert-test
   docker rm -f talaia-alert-test
   ```

### Llenar el disco raíz con seguridad

Ejecútalo solo en un servidor de pruebas desechable. Lleva temporalmente el sistema de archivos raíz a alrededor
del 91 %, justo por encima de la regla por defecto, y borra el archivo al salir de la shell:

```bash
file=/talaia-disk-alert-test
trap 'sudo rm -f "$file"' EXIT
total=$(df --output=size --block-size=1 / | awk 'NR==2 {print $1}')
used=$(df --output=used --block-size=1 / | awk 'NR==2 {print $1}')
size=$(( total * 91 / 100 - used + 100 * 1024 * 1024 ))
if [ "$size" -le 0 ]; then
  echo "El disco ya está al 91 % o más; no hay nada que hacer."
elif [ "$(( total - used - size ))" -lt 1073741824 ]; then
  echo "Me niego a dejar menos de 1 GiB libre."
else
  sudo fallocate -l "$size" "$file"
fi
```

Espera la alerta de disco y pulsa Ctrl-D (o ejecuta `rm`) para borrar el archivo y dejar que la alerta se
resuelva. Si `fallocate` no está disponible, detente aquí en vez de llenar un disco de producción con `dd`.
Deja al menos 1 GiB libre para el núcleo y PostgreSQL.

## Solución de problemas

**No llega ninguna alerta**

1. Abre **Alertas > Activas** y comprueba que el incidente existe. Si no existe, la regla está desactivada, su
   ámbito no coincide, la condición aún no es cierta o no ha pasado la duración pendiente.
2. Abre **Ajustes > Alertas > Canales** y lee el último estado. Usa **Enviar prueba**; el error censurado
   aparece ahí.
3. Revisa la lista de canales de la regla. Una lista vacía significa todos los canales activos; una lista con
   elementos excluye todos los canales no seleccionados.
4. Revisa **Silencios**; un silencio activo suprime el envío, no el incidente.
5. Si el destino está en una dirección privada, la protección de salida devuelve `blocked address` o
   `connection failed`. Activa **Permitir que las comprobaciones y las notificaciones lleguen a redes privadas**
   solo si es lo que quieres; las direcciones de metadatos y link-local siguen bloqueadas.
6. En alertas de contenedor, confirma que el contenedor se vio en ejecución. Los contenedores parados o
   eliminados desde Talaia dejan de vigilarse a propósito.
7. Para `server_offline`, espera a pasar el margen de arranque de 90 segundos. Para `check_failed`, confirma
   que la comprobación está activa y que su último resultado falla.

**El núcleo falla al arrancar con un error de migración**

Talaia necesita PostgreSQL con la extensión **TimescaleDB**. La migración `0014_metrics.sql` ejecuta
`CREATE EXTENSION IF NOT EXISTS timescaledb`; un PostgreSQL normal responde con un error como
`extension "timescaledb" is not available` (y un archivo `timescaledb.control` que falta). El
`packaging/core/compose.yaml` empaquetado usa `timescale/timescaledb:latest-pg16`, que ya la incluye. Comprueba:

```bash
sudo docker compose logs --tail=100 db core
```

No apuntes el núcleo a una imagen PostgreSQL normal esperando que las métricas funcionen.


