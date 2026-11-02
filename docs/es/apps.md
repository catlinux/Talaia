# Apps y despliegues

[Read in English](../en/apps.md) · [Volver al README](../../README.es.md)

Una app en Talaia es un proyecto Docker Compose normal en un servidor. Sale de un repositorio de GitHub
con un Dockerfile, de una plantilla o de una imagen que ya existe. Talaia guarda la configuración del
origen y el historial de despliegues, y crea o actualiza el stack Compose por ti. No hay un formato oculto:
el stack sigue en disco y funciona con `docker compose` aunque desinstales Talaia.

Esta guía explica las pantallas de apps, Git, las variables, los despliegues y los dos modos de
publicación. Para la configuración paso a paso de Apache o nginx en un servidor que ya tiene un servidor
web, consulta [apache.md](apache.md). El mapa de pantallas está en [panel.md](panel.md); los permisos,
en [permisos.md](permisos.md).

## Contenido

[Qué es una app](#qué-es-una-app) · [Permisos](#permisos) · [Crear una app](#crear-una-app) ·
[Repositorios Git](#repositorios-git) · [Plantillas](#plantillas) · [Imágenes existentes](#imágenes-existentes) ·
[Variables](#variables) · [Despliegues, registros, reintentar y volver atrás](#despliegues-registros-reintentar-y-volver-atrás) ·
[Dominios y publicación](#dominios-y-publicación) · [Qué toca Talaia](#qué-toca-talaia) ·
[Solución de problemas](#solución-de-problemas)

## Qué es una app

Una app tiene un nombre, un origen y un servidor. El nombre sigue las mismas reglas que un stack: letras
minúsculas, números, guiones y guiones bajos, hasta 63 caracteres, y debe ser único en ese servidor.
También es el nombre del proyecto Compose y el prefijo de la imagen `talaia/<nombre>`.

Talaia guarda la app en `/opt/talaia/stacks/<nombre>/compose.yaml`. En modo externo, el primer despliegue
también asigna un puerto del anfitrión entre 20000 y 29999 en `127.0.0.1`, así que la app queda accesible
en `127.0.0.1:<puerto-del-anfitrión>` en el servidor y en ningún otro sitio hasta que tu servidor web la
publique. En modo gestionado la app se une a la red `talaia-proxy` y Caddy la publica en los puertos 80 y
443.

La fila de **Apps** muestra su origen, endpoint, si despliega en push y cuándo cambió. Ábrela para ver
**Configuración**, **Despliegues** y **Variables**.

## Permisos

| Permiso | Qué permite |
| --- | --- |
| `stack.view` | Ver la app, su configuración, los nombres y marcas de sus variables (los valores secretos siguen ocultos) y su lista de despliegues. |
| `stack.deploy` | **Desplegar ahora**, **Reintentar**, **Volver atrás** y leer el registro de un despliegue. Las claves de API pueden tenerlo para CI. |
| `stack.edit` | Crear, cambiar y eliminar la app, sus variables y sus dominios. Equivale a root en el servidor. Las claves de API no pueden tenerlo. |
| `server.proxy` | Ver el modo del proxy, el estado de los certificados y el bloque de configuración externo en **Dominios**; usar la tarjeta **Publicación de apps** e instalar, aplicar o quitar el proxy gestionado. Las claves de API no pueden tenerlo. |

Quien puede editar un archivo Compose o una app ya controla lo que se ejecuta como root en ese servidor.
Da `stack.edit` y `server.proxy` solo a personas en las que confiarías root. Las reglas completas de los
roles están en [permisos.md](permisos.md).

## Crear una app

Abre **Apps** y pulsa **Nueva app**. El asistente tiene cinco pasos: **Origen**, **Construcción**,
**Destino**, **Dominio** y **Revisión**. El resumen de la derecha se actualiza al avanzar.

1. **Origen** — elige **Repositorio Git**, **Plantilla** o **Imagen existente**.
2. **Construcción** — elige el repositorio y el Dockerfile, la plantilla o la imagen; indica el servicio
   de Compose y el puerto del contenedor. Los valores por defecto son el servicio `app`, el puerto `3000`,
   el Dockerfile `Dockerfile` y el contexto `.`.
3. **Destino** — elige un servidor donde puedas crear apps, pon el nombre de la app y decide si despliega
   en cada push. Solo aparecen los servidores donde tienes `stack.edit`.
4. **Dominio** — añade opcionalmente el nombre de host. El paso muestra si el servidor usa un proxy externo
   o el proxy gestionado de Caddy, y explica quién publica la app. Talaia nunca edita Apache ni nginx.
5. **Revisión** — revisa el resumen y pulsa **Crear la app**.

El nombre de la app queda fijado al crearla; también es el directorio del stack y el nombre de la imagen.
Elige con cuidado.

Cuando la app está creada, el panel muestra **App creada**. El primer despliegue se pone en cola para las
apps de Git y de imagen. Una app de plantilla también se despliega si tienes `stack.deploy`; si no, el
panel avisa de que alguien con permiso de despliegue tiene que arrancarla desde la ficha de la app. En una
app de Git, copia ahora los valores de esta pantalla: no se vuelven a mostrar.

El campo **Nombre de host** es opcional. En esta versión los dominios se añaden desde el asistente; no hay
otro formulario en el panel para añadir uno después. Si lo dejas vacío, la app se despliega igualmente,
pero en un servidor externo no hay bloque de Apache hasta que exista un dominio. Un aviso como **El nombre
de host no resuelve.** o **El nombre de host no apunta a este servidor.** no impide crear la app; corrige el
registro DNS antes de pedir el certificado.

Tras crear una app con dominio en un servidor externo, la pantalla de éxito muestra un aviso destacado de que la app **todavía no está publicada**, con los pasos para añadir el sitio de Apache y el certificado a mano.

## Repositorios Git

Talaia construye las apps de Git en el servidor a partir de un Dockerfile. Solo acepta repositorios de
`github.com`, por HTTPS, `ssh://git@github.com/...` o `git@github.com:...`. El contexto de construcción y
la ruta del Dockerfile son relativos al repositorio y no pueden salir de él.

El paso **Construcción** pide **URL del repositorio**, **Rama**, **Ruta del Dockerfile** y **Contexto de
construcción**. El repositorio se descarga en la punta actual de la rama; no se usan submódulos ni hooks
de Git.

Para un repositorio privado, añade la clave de despliegue a GitHub. Para uno público, la descarga funciona
sin clave, pero la app también tiene una.

### Clave de despliegue

Al crear una app de Git, Talaia genera un par de claves ed25519. La pantalla de éxito muestra una sola vez
la **Clave pública de despliegue**. En el repositorio de GitHub abre **Settings > Deploy keys > Add deploy
key**, pega la clave y deja **Allow write access** desactivado. Talaia solo lee el repositorio.

La clave privada se sella en el núcleo y nunca vuelve por la API ni se escribe en un registro. El agente
usa un conjunto fijo de claves de host de GitHub y las comprueba de forma estricta. Si el repositorio es
privado y la clave no está añadida, el primer despliegue falla al descargar el origen; añade la clave y
pulsa **Desplegar ahora** otra vez.

### Webhook

Una app de Git también recibe una **URL del webhook** y un **Secreto del webhook**, que se muestran una
sola vez en la pantalla de éxito. En GitHub abre **Settings > Webhooks > Add webhook** y rellena:

- **Payload URL**: la URL que muestra Talaia. Termina en `/api/v1/hooks/git/<id-de-la-app>`.
- **Content type**: `application/json`.
- **Secret**: el valor que muestra Talaia.
- **SSL verification**: activada.
- **Which events would you like to trigger this webhook?**: **Just the push event**.
- **Active**: activado.

GitHub envía un `ping` al crear el webhook; Talaia lo responde sin desplegar nada. Solo un push a la rama
configurada despliega. Un push a otra rama, una rama borrada o una entrega duplicada se ignoran. El webhook
solo despliega mientras **Despliega en cada push** está activado.

El endpoint es público porque GitHub tiene que llegar a él; la petición se autentica con la firma
`X-Hub-Signature-256` HMAC-SHA256 hecha con el secreto de la app. Trata el secreto como una contraseña y no
lo reutilices. Si cambia la URL pública del panel, actualiza la Payload URL en GitHub.

### Sondeo

Si GitHub no puede llegar al panel, activa **También sondea Git cada 2 minutos** en el paso **Destino** en
lugar de usar un webhook. El sondeo consulta la rama desde el núcleo y pone un despliegue en cola cuando
cambia el commit. Usa la misma clave de despliegue para repositorios privados y también necesita
**Despliega en cada push** activado. La primera comprobación solo guarda el commit actual; los cambios
posteriores se despliegan en unos dos minutos.

### Pushes

Por defecto, una app de Git tiene **Despliega en cada push** activado. Cada push a la rama configurada pone
un despliegue en cola. Si ya hay un despliegue esperando para esa app, el nuevo marca el que espera como
**Sustituido**; el despliegue que ya está en marcha no se interrumpe.

Desactiva **Despliega en cada push** si quieres desplegar solo desde la ficha de la app o desde CI con una
clave de API. En ese modo se ignoran los eventos del webhook y del sondeo.

## Plantillas

**Plantillas** es una galería de apps preparadas. Busca por nombre, imagen o descripción; cada tarjeta
muestra la imagen, el puerto del contenedor y la RAM recomendada. Pulsa **Usar la plantilla** para abrir el
asistente de nueva app con la plantilla ya elegida; el nombre de la app se rellena a partir del id de la
plantilla y se puede cambiar.

El catálogo tiene quince aplicaciones —Forgejo, FreshRSS, Ghost, Homepage, Immich, Jellyfin, Mealie,
Miniflux, Nextcloud, Paperless-ngx, Plausible, Umami, Uptime Kuma, Vaultwarden y WordPress— y tres bloques:
PostgreSQL, MariaDB y Redis. Las imágenes están fijadas.

Talaia genera un stack Compose para la plantilla con contraseñas aleatorias. La contraseña completa de la
plantilla se guarda como secreto y no se muestra en el panel. Una plantilla no construye nada: el primer
despliegue descarga la imagen fijada y arranca el stack. La RAM que se muestra es una recomendación, no un
límite.

Algunas plantillas declaran una credencial de primer inicio de sesión. Al crear una, la pantalla **App
creada** muestra su usuario y su contraseña una sola vez; cópialos entonces. Talaia no vuelve a mostrar esa
contraseña y no se puede recuperar después. Una plantilla sin `credential_length` no tiene credencial de
primer inicio de sesión.

Un archivo de plantilla, `templates/<id>/template.yaml`, contiene los campos de metadatos, `image`,
`service`, `port`, `ram_mb` y el bloque `compose`. Dos claves opcionales describen la credencial de primer
inicio de sesión:

- `credential_length`: la `N` del marcador `{{password:N}}` que es el secreto de acceso, un entero de 8 a
  96. Sin esta clave, la plantilla no tiene credencial de primer inicio de sesión.
- `credential_user`: el nombre de usuario que se muestra con la contraseña, de hasta 64 caracteres y sin
  caracteres de control. Es opcional cuando el servicio no usa usuario; requiere `credential_length`.

Las plantillas incluidas publican en `127.0.0.1` y solo funcionan en modo externo: en un servidor con el
proxy en modo gestionado el despliegue falla con un mensaje que lo explica y no se arranca nada. Una plantilla
desplegada vuelve a generar su archivo Compose a partir de la plantilla, así que los cambios que hagas a
mano en ese stack se sustituyen en el siguiente despliegue. Si necesitas conservar cambios, usa una app de
imagen o de Git, o mantén un stack aparte.

## Imágenes existentes

Elige **Imagen existente** para ejecutar una imagen que ya está publicada. Indica una referencia fijada,
como `ghcr.io/owner/app:1.2.3`, el servicio de Compose y el puerto del contenedor. Talaia no construye
nada y no usa clave de despliegue ni webhook; genera el stack y arranca la imagen.

El servidor tiene que poder descargar la imagen. Esta versión no guarda credenciales de registro para las
apps, así que una imagen privada necesita la autenticación de Docker que ya esté configurada en el
servidor.

## Variables

Abre una app y usa **Variables**. **Añadir variable** pide una **Clave**, un **Valor**, **Secreto** y
**Disponible en tiempo de compilación**.

- Las claves usan letras mayúsculas, números y guiones bajos, empiezan por letra o guion bajo y tienen como
  máximo 128 caracteres.
- Los valores **Secreto** son de solo escritura y se sellan en el núcleo. El panel los oculta y nunca
  devuelve el valor; para sustituirlo, introduce un valor nuevo. Eliminar una variable la quita de la app.
- **Disponible en tiempo de compilación** pasa una variable no secreta a `docker build` como argumento de
  construcción. Las variables secretas nunca se pasan como argumentos, aunque la marca esté activada.
- Talaia es dueña del archivo `.env`: cada despliegue lo vuelve a generar a partir de las variables de la
  app. El stack generado y un stack de plantilla lo leen con `env_file: [.env]`. `TEMPLATE_PASSWORD` es
  interna de una plantilla y no se exporta. En una plantilla con credencial de primer inicio de sesión, el
  panel muestra esa credencial una sola vez al crearla y no puede recuperarla después. Edita las variables
  en el panel, no en `.env`, porque el siguiente despliegue sustituye el archivo.
- Si la app tiene variables y un stack editado a mano no referencia `.env`, Talaia escribe igualmente el
  archivo pero deja un aviso en el registro. Añade `env_file: [.env]` al servicio de la app de ese stack
  para que el contenedor reciba las variables.

Cambiar una variable no modifica por sí solo un contenedor en marcha. Vuelve a desplegar la app: el valor
queda disponible en ejecución y, cuando **Disponible en tiempo de compilación** está activado, en la
siguiente construcción.

## Despliegues, registros, reintentar y volver atrás

Se crea un despliegue al crear la app, cuando un push o el sondeo encuentra un commit nuevo, con
**Desplegar ahora**, con **Reintentar** o con **Volver atrás**. Hay un despliegue a la vez por app; uno
nuevo sustituye al que todavía está esperando.

La canalización descarga el origen, construye la imagen cuando el origen es Git, escribe el stack, lo
publica y, en las construcciones de Git, limpia imágenes antiguas. La ficha de la app lista los despliegues
con su estado: **En cola**, **Construyendo**, **Publicando**, **Correcto**, **Fallido** o **Sustituido**.

### Construcciones

- Las apps de Git se construyen en el servidor de la app con `docker build --pull` y la etiqueta
  `talaia/<app>:<primeros-12-caracteres-del-commit>`.
- Se ejecuta una construcción a la vez en un servidor.
- La construcción se rechaza cuando el sistema de archivos del espacio de trabajo tiene menos de 2 GiB
  libres. El espacio del origen está limitado a 1 GiB y se elimina después de construir.
- Una construcción tiene un límite de 30 minutos.
- Solo las variables no secretas marcadas **Disponible en tiempo de compilación** se convierten en
  argumentos de construcción, hasta 128.
- Las apps de plantilla y de imagen no construyen.

### Registros y reintentar

Abre **Registro** en un despliegue para ver la salida en un panel lateral. Mientras el despliegue está
**En cola**, **Construyendo** o **Publicando**, el panel muestra **En directo** y se actualiza. El registro
guarda los últimos 256 KiB; la salida más antigua se descarta por el principio. Los registros necesitan
`stack.deploy`.

**Reintentar** aparece en un despliegue fallido con commit. Vuelve a poner en cola el mismo commit. Una
construcción fallida no toca el stack en marcha. Si falla una publicación de Git, Talaia restaura el archivo
Compose anterior.

### Sin corte

**Sin corte** solo está disponible cuando se cumplen todas estas condiciones:

- la app tiene **Sin corte** activado;
- el servidor usa el proxy gestionado;
- el stack tiene un solo servicio;
- ese servicio no tiene volúmenes, ni puertos publicados en el anfitrión, ni un `container_name` fijo;
- el servicio no pide más de una réplica.

Talaia arranca un segundo contenedor, espera hasta 120 segundos al healthcheck de Docker o, si no hay
healthcheck, a una conexión TCP al puerto de la app, elimina el contenedor viejo y vuelve a una réplica. Si
el nuevo no llega a estar sano, se elimina y el viejo sigue sirviendo; el despliegue queda **Fallido**.

El modo externo publica la app en `127.0.0.1`, así que el segundo contenedor no puede usar el mismo puerto
del anfitrión. Por eso cada despliegue detrás de tu propio Apache o nginx tiene un corte de unos segundos.
La ficha de la app lo indica bajo **Sin corte**.

### Imágenes y volver atrás

Las construcciones correctas de Git se guardan como las tres últimas imágenes de `talaia/<app>`. Las más
antiguas se pueden eliminar después de una publicación. **Volver atrás** en un despliegue correcto vuelve
a desplegar la imagen de ese commit sin construir; necesita que la imagen siga existiendo. Talaia solo
elimina imágenes cuyo nombre empieza por `talaia/<app>:`. Nunca ejecuta un prune global de Docker ni
elimina imágenes de otra app.

Después del primer despliegue, una app de Git o de imagen cuyo archivo Compose se haya editado a mano
conserva el resto del archivo; cada despliegue cambia solo el `image:` del servicio configurado. Una app de
plantilla se vuelve a generar a partir de su plantilla.

## Dominios y publicación

**Dominios** lista los nombres de host de las apps que puedes ver. La columna **Estado** muestra:

- **Lo gestiona tu Apache** en un servidor externo: Talaia no gestiona el certificado.
- **Gestionado por Caddy** en un servidor gestionado cuando no hay datos del certificado.
- **Certificado válido**, **Certificado pendiente** o **Certificado fallido** en un dominio gestionado
  con datos del certificado.
- **Desconocido** cuando no se puede determinar el estado.

Un aviso dice **El nombre de host no resuelve.** o **El nombre de host no apunta a este servidor.** cuando
falla la comprobación de DNS. La comprobación no bloquea la app.

El estado del proxy y el bloque de configuración externo necesitan `server.proxy`. Con solo `stack.view`
el dominio puede seguir en la lista, pero muestra **Desconocido** y no tiene el botón **Ver la
configuración**.

### Proxy externo

El modo externo es el de por defecto. La app escucha en `127.0.0.1:<puerto-del-anfitrión>` y tu propio
Apache o nginx la publica. Talaia nunca edita esos archivos. El primer despliegue asigna el puerto del
anfitrión entre 20000 y 29999; hasta entonces el bloque de configuración no está listo.

En **Dominios**, abre **Ver la configuración** en una fila con **Lo gestiona tu Apache**. El panel muestra
el nombre de host y `127.0.0.1:<puerto-del-anfitrión>`, el bloque de **Apache**, el de **nginx** y los
**Pasos**. Se aplica la misma nota: *Talaia nunca cambia Apache ni nginx. Pega tú el bloque en la
configuración del servidor web.* Usa **Copiar** para copiar un bloque o los pasos; el botón cambia a
**Copiado**.

La guía completa, con certificados y eliminación, está en [apache.md](apache.md).

### Proxy gestionado (Caddy)

> **Opcional, solo para servidores vacíos.** La mayoría de servidores ya sirven una web en los puertos 80 y
> 443, así que el modo externo de arriba es el normal. El modo gestionado todavía no se ha probado de punta a
> punta en un servidor real; solo se comprueba su configuración de Caddy con `caddy validate`.

En un servidor donde los puertos 80 y 443 están libres, la tarjeta **Publicación de apps** de la página
del servidor puede instalar un proxy Caddy gestionado. Caddy 2.10.2 está fijado por digest y escucha solo
en esos puertos; las apps se unen a la red `talaia-proxy` y Caddy obtiene un certificado para cada nombre
de host automáticamente.

Instalar necesita `server.proxy`. La tarjeta solo ofrece **Instalar el proxy gestionado** cuando el agente
informa de que los dos puertos están libres; si uno está en uso o el agente no puede confirmarlo, lo dice y
el modo externo sigue siendo la opción. Después de cambiar un dominio, usa **Aplicar las rutas** para
cargar la nueva configuración de Caddy. **Eliminar el proxy gestionado** detiene Caddy y elimina su stack,
pero conserva las apps y sus datos.

Elige el modo antes de crear apps. Instalar o quitar el proxy no reescribe los stacks existentes editados a
mano, y las plantillas incluidas están pensadas para el modo externo.

## Qué toca Talaia

- El proyecto Compose de la app en `/opt/talaia/stacks/<app>/`, incluido `compose.yaml` y el `.env`
  generado a partir de las variables de la app en cada despliegue.
- Los espacios de construcción en `/var/lib/talaia/builds/<app>/`, que se eliminan después de cada
  construcción.
- Las imágenes llamadas `talaia/<app>:<commit>`. Guarda las tres últimas correctas y elimina solo otras
  imágenes con ese prefijo exacto.
- En modo gestionado, el stack de Caddy en `/var/lib/talaia/proxy/` y la red `talaia-proxy`.
- La etiqueta de contenedor `dev.talaia.app` y, en modo externo, un puerto del anfitrión entre 20000 y
  29999 en `127.0.0.1`.

**No** toca Apache, nginx, tus otros proyectos Compose, otros contenedores ni otras imágenes. Los stacks
que Talaia genera para apps de Git y de imagen no montan el socket de Docker, no se ejecutan como
privilegiados y fijan un límite de 1 CPU y 512 MB de memoria. Quien tiene `stack.edit` puede cambiarlo, y
por eso el permiso equivale a root. Talaia nunca ejecuta un prune global de Docker.

Para eliminar una app, ábrela y pulsa **Eliminar la app** (necesita `stack.edit`). Escribe el nombre de
la app para confirmar. Talaia detiene y elimina sus contenedores y el stack Compose, borra sus imágenes y
su espacio de construcción, conserva los volúmenes con nombre y después elimina la app. El sitio web
externo sigue siendo tuyo; el procedimiento completo está en
[apache.md](apache.md#eliminar-una-app-y-su-sitio).

## Solución de problemas

**El primer despliegue ha fallado.** Comprueba que el repositorio es accesible, que un repositorio privado
tiene la clave de despliegue, que existen la ruta del Dockerfile y el contexto de construcción, y que el
servidor tiene al menos 2 GiB libres. Abre **Registro** en el despliegue fallido; la salida indica el paso
que falla.

**Un push no despliega.** Comprueba que **Despliega en cada push** está activado, que el webhook apunta a
la URL actual, que **Content type** es `application/json`, que el secreto coincide y que el push es a la
rama configurada. Las entregas recientes de GitHub muestran la respuesta. Si usas sondeo, comprueba que
**También sondea Git cada 2 minutos** también está activado.

**El dominio muestra un aviso.** Crea el registro DNS del nombre de host y apúntalo al servidor. La app se
puede crear antes de que el DNS esté listo, pero la emisión del certificado espera.

**El sitio devuelve 502.** La app o su contenedor no responde en el puerto del anfitrión. Consulta
[apache.md](apache.md#solución-de-problemas).

**No se puede emitir el certificado.** Comprueba el registro DNS, que los puertos 80 y 443 llegan al
servidor y que el nombre de host del sitio de Apache o nginx es el mismo que pediste. Los comandos y los
registros están en [apache.md](apache.md#solución-de-problemas).

**Falla un retorno.** Puede que la imagen de ese despliegue ya se haya eliminado. Talaia guarda las tres
últimas imágenes correctas; vuelve a desplegar el commit si necesitas uno más antiguo.

**No se puede descargar una imagen.** Esta versión no guarda credenciales de registro. Configura la
autenticación de Docker en el servidor o usa una imagen pública.
