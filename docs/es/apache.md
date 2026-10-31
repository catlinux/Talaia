# Publicar una app detrás de Apache o nginx

[Read in English](../en/apache.md) · [Volver al README](../../README.es.md)

Esta guía es para el **modo externo**: un servidor que ya ejecuta Apache o nginx con otros sitios, donde
Talaia publica cada app en `127.0.0.1` y tú añades el sitio a mano. Talaia nunca cambia Apache, nginx ni
sus certificados. Si el servidor puede ceder los puertos 80 y 443 a Talaia, el modo gestionado con Caddy
se explica en [apps.md](apps.md#proxy-gestionado-caddy); entonces no necesitas esta guía.

La app se explica en [apps.md](apps.md). El mapa de pantallas está en [panel.md](panel.md).

## Contenido

[Antes de empezar](#antes-de-empezar) · [1. Apuntar el dominio al servidor](#1-apuntar-el-dominio-al-servidor) ·
[2. Crear la app y su dominio](#2-crear-la-app-y-su-dominio) ·
[3. Esperar al primer despliegue](#3-esperar-al-primer-despliegue) ·
[4. Copiar el bloque de configuración](#4-copiar-el-bloque-de-configuración) ·
[5. Guardar y activar el sitio de Apache](#5-guardar-y-activar-el-sitio-de-apache) ·
[6. Obtener el certificado](#6-obtener-el-certificado) · [7. Verificar](#7-verificar) ·
[8. Desplegar en cada push](#8-desplegar-en-cada-push) · [Variante de nginx](#variante-de-nginx) ·
[Eliminar una app y su sitio](#eliminar-una-app-y-su-sitio) · [Solución de problemas](#solución-de-problemas)

## Antes de empezar

- Un registro DNS del nombre de host de la app que apunte al servidor.
- Apache instalado y en marcha, con los puertos 80 y 443 accesibles desde internet.
- `certbot` y su plugin de Apache, o permiso para instalarlos.
- `stack.edit` en el servidor de la app para crearla; `server.proxy` en ese servidor para ver el modo del
  proxy y copiar el bloque de configuración en **Dominios**. `stack.view` permite ver la app y sus
  despliegues, pero no el bloque.
- El servidor en modo externo. Talaia nunca instala Apache ni nginx ni edita sus archivos.

Los ejemplos de esta guía usan el nombre de host `app.example.com` y la dirección de servidor
`192.0.2.10`.

## 1. Apuntar el dominio al servidor

Crea un registro **A** para `app.example.com` con la dirección IPv4 del servidor. Si el servidor tiene
IPv6, añade también un registro **AAAA**. Con las herramientas habituales:

```
dig +short app.example.com
```

La respuesta debe contener la dirección del servidor, por ejemplo `192.0.2.10`. Los cambios de DNS pueden
tardar unos minutos. Talaia lo comprueba al añadir el dominio y muestra **El nombre de host no resuelve.**
o **El nombre de host no apunta a este servidor.** si no puede confirmarlo. Puedes crear la app antes de
que el DNS esté listo, pero la emisión del certificado espera.

## 2. Crear la app y su dominio

Abre **Apps** y pulsa **Nueva app**. Elige el origen, la construcción y el destino como se describe en
[apps.md](apps.md#crear-una-app). En el paso **Dominio**:

1. Escribe `app.example.com` en **Nombre de host**.
2. Comprueba que el paso dice **Proxy externo**. La app escucha entonces en `127.0.0.1` y tu servidor web
   la publica.
3. Continúa a **Revisión** y pulsa **Crear la app**.

El primer despliegue empieza automáticamente para una app de Git o de imagen. Una app de plantilla empieza
automáticamente si tienes `stack.deploy`; si no, abre la app y pulsa **Desplegar ahora** con una cuenta que
tenga ese permiso.

Si ya creaste la app sin dominio, en esta versión no hay otro formulario en el panel. Añádelo por la API
de apps (`POST /api/v1/apps/<id-de-la-app>/domains`) con una sesión iniciada y un cuerpo JSON como
`{"hostname":"app.example.com"}`, y continúa con el primer despliegue. El nombre de host debe ser válido y
único.

## 3. Esperar al primer despliegue

El bloque de configuración necesita un puerto del anfitrión, y el puerto se asigna en el primer
despliegue. Abre la app y comprueba:

- **Despliegues** muestra un despliegue **Correcto**.
- **Configuración** muestra un **Puerto del anfitrión**, por ejemplo `20431`, y un **Endpoint** como
  `127.0.0.1:20431  app:3000`.

Si el despliegue ha fallado, abre **Registro** y corrige la causa antes de continuar. Las causas habituales
son una clave de despliegue que falta, una ruta de Dockerfile o un contexto de construcción equivocados,
un puerto del contenedor en el que la app no escucha, o menos de 2 GiB libres en el servidor.

En modo externo el puerto del anfitrión se elige entre 20000 y 29999. Pertenece a esta app; no lo
reutilices en otro stack.

## 4. Copiar el bloque de configuración

Abre **Dominios** y busca `app.example.com`. Un dominio externo muestra **Lo gestiona tu Apache**, aunque
el servidor web sea nginx. Pulsa **Ver la configuración**.

El panel muestra:

- el nombre de host y `127.0.0.1:<puerto-del-anfitrión>`;
- el bloque de **Apache**;
- el bloque de **nginx**;
- los **Pasos** con los comandos para los dos servidores web.

Lee la nota: *Talaia nunca cambia Apache ni nginx. Pega tú el bloque en la configuración del servidor
web.* Usa **Copiar** en el bloque de Apache y luego **Copiar** en los pasos si los quieres en una terminal.
El botón cambia a **Copiado**.

El bloque es solo para el puerto 80. `certbot --apache` escribe el host virtual HTTPS y el certificado
después de que funcione el sitio HTTP. No añadas `SSLCertificateFile` a mano antes de que exista el
certificado.

## 5. Guardar y activar el sitio de Apache

Crea el archivo del sitio con el bloque de Apache del panel:

```
sudo nano /etc/apache2/sites-available/app.example.com.conf
```

Pega el bloque, guarda y sal. Después activa los módulos que necesita y el sitio. El comando de módulos se
puede repetir sin problema si alguno ya está activo.

```
sudo a2enmod proxy proxy_http proxy_wstunnel headers rewrite
sudo a2ensite app.example.com
sudo apachectl configtest && sudo systemctl reload apache2
```

`a2ensite` recibe el nombre del sitio sin `.conf`. `configtest` debe terminar con `Syntax OK`; si informa
de un error, corrige el archivo antes de recargar. Una recarga mantiene en marcha tus otros sitios.

El bloque usa `ProxyPreserveHost`, reenvía el protocolo original y actualiza las conexiones WebSocket. No
toques los otros hosts virtuales: Talaia no los ha creado ni los cambia.

## 6. Obtener el certificado

Comprueba otra vez que el nombre de host resuelve a este servidor y que el puerto 80 responde. Luego
instala Certbot si no está ya:

```
sudo apt update
sudo apt install certbot python3-certbot-apache
```

Pide el certificado:

```
sudo certbot --apache -d app.example.com
```

Certbot pide un correo y la aceptación la primera vez. Elige la redirección a HTTPS cuando la ofrezca.
Escribe el host virtual HTTPS y configura la renovación. No copies el bloque del puerto 80 a
`default-ssl` ni añadas el certificado a mano.

Comprueba el resultado y el temporizador de renovación:

```
sudo certbot certificates
systemctl list-timers | grep certbot
```

## 7. Verificar

```
curl -I http://app.example.com
curl -I https://app.example.com
```

La petición HTTP debe redirigir a HTTPS, y la petición HTTPS debe devolver el estado de la app, normalmente
`200`. Abre `https://app.example.com` en un navegador y comprueba que la app funciona, incluida cualquier
función interactiva o WebSocket que uses.

En el servidor, `sudo apachectl -S` lista los hosts virtuales y el archivo que sirve cada nombre. Es útil
cuando otro sitio responde primero.

## 8. Desplegar en cada push

Con **Despliega en cada push** activado y el webhook de GitHub o el sondeo configurados, un push a la rama
configurada pone un despliegue nuevo en cola. La ficha de la app lo muestra en **Despliegues**; la
configuración de Apache o nginx no cambia. Cada despliegue externo tiene un corte de unos segundos porque
la app publica un puerto fijo del anfitrión. Si el despliegue falla, el contenedor viejo sigue sirviendo.

Si despliegas a mano, pulsa **Desplegar ahora** y espera a **Correcto** antes de comprobar el sitio.

## Variante de nginx

El mismo panel da el bloque de nginx. Úsalo cuando nginx ocupa los puertos 80 y 443:

```
sudo nano /etc/nginx/sites-available/app.example.com
```

Pega el bloque de **nginx**, guárdalo y activa el sitio:

```
sudo ln -s /etc/nginx/sites-available/app.example.com /etc/nginx/sites-enabled/
sudo nginx -t && sudo systemctl reload nginx
```

Luego pide el certificado con el plugin de nginx:

```
sudo certbot --nginx -d app.example.com
```

`nginx -t` debe pasar antes de la recarga. Certbot escribe el servidor HTTPS y la redirección. El resto de
tus sitios de nginx no se tocan.

## Eliminar una app y su sitio

Esta versión no tiene un botón **Eliminar app** en el panel. Elimina el registro de la app por la API y
después quita el sitio y el stack en este orden. El id de la app es la última parte de la dirección de su
ficha, `/apps/<id>`. Con la sesión iniciada en el panel, ejecuta esto en la consola del navegador en la
pestaña del panel:

```js
fetch('/api/v1/apps/<app-id>', {
  method: 'DELETE',
  headers: { 'Content-Type': 'application/json' },
})
```

Necesita `stack.edit` en la app y usa tu sesión actual; una clave de API no puede eliminar una app. Devuelve
`204` cuando funciona. Elimina la app, sus variables, su historial de despliegues y sus dominios. **No**
detiene los contenedores, no elimina el stack Compose ni cambia el sitio web.

Elimina el sitio de Apache:

```
sudo a2dissite app.example.com
sudo rm /etc/apache2/sites-available/app.example.com.conf
sudo apachectl configtest && sudo systemctl reload apache2
```

Si ningún otro sitio usa el certificado, elimínalo también:

```
sudo certbot delete --cert-name app.example.com
```

Consérvalo si otro host virtual sigue usando el mismo nombre. Para nginx, quita el enlace y el archivo:

```
sudo rm /etc/nginx/sites-enabled/app.example.com
sudo rm /etc/nginx/sites-available/app.example.com
sudo nginx -t && sudo systemctl reload nginx
```

Después elimina el stack. Abre el servidor, ve a **Contenedores**, busca el stack con el nombre de la app
en **Stacks** y elige **Eliminar**. Confirma el diálogo. Talaia detiene y elimina sus contenedores y borra
los archivos Compose; conserva los volúmenes, así que los datos de la app no se destruyen.

La acción del stack no elimina las imágenes `talaia/<app>:*`. Si ya no necesitas las copias para volver
atrás, lístalas y elimínalas en el servidor:

```
sudo docker image ls talaia/<app>
sudo docker image rm talaia/<app>:<etiqueta>
```

El registro DNS puede quedarse si vas a reutilizar el nombre; si no, elimínalo en tu proveedor de DNS.

## Solución de problemas

### El sitio devuelve 502 Bad Gateway

El servidor web llegó al puerto del anfitrión pero la app no respondió. Comprueba, en este orden:

1. El último despliegue está **Correcto**; un despliegue fallido puede dejar el contenedor viejo sirviendo.
2. El puerto del bloque de Apache o nginx coincide con **Puerto del anfitrión** en la ficha de la app. Si
   la app se recreó o el bloque es antiguo, vuelve a copiarlo desde **Dominios > Ver la configuración**.
3. En el servidor, la app escucha en el puerto del anfitrión:

   ```
   sudo ss -ltnp | grep <puerto-del-anfitrión>
   curl -I http://127.0.0.1:<puerto-del-anfitrión>
   ```

4. El contenedor está en marcha y sus registros no muestran un fallo:

   ```
   cd /opt/talaia/stacks/<app>
   sudo docker compose ps
   sudo docker compose logs --tail=100
   ```

5. La app escucha en todas las interfaces dentro del contenedor, no solo en `127.0.0.1` dentro de él.
   Docker reenvía a la dirección del contenedor, así que un proceso enlazado solo al loopback del
   contenedor no es accesible por el puerto publicado.

### No se emite el certificado

- `dig +short app.example.com` debe devolver la dirección del servidor. Quita un registro AAAA
  equivocado si IPv6 no llega al servidor.
- El puerto 80 debe responder desde internet. Revisa el cortafuegos y el grupo de seguridad de tu
  proveedor.
- El `ServerName` del bloque de Apache, o `server_name` en nginx, debe ser exactamente el nombre que pasas
  a Certbot.
- Lee `/var/log/letsencrypt/letsencrypt.log` y ejecuta `sudo certbot certificates`.
- Si has alcanzado un límite de peticiones, espera y vuelve a intentarlo. No borres el sitio HTTP que
  funciona mientras depuras.
- Después de una renovación correcta, `sudo certbot renew --dry-run` comprueba el camino de renovación sin
  cambiar el certificado.

### Responde otro sitio o la app muestra un 404

Ejecuta `sudo apachectl -S` y comprueba qué archivo sirve `app.example.com`. El sitio debe estar activado,
cargado y usar el mismo nombre. En nginx, `sudo nginx -T` muestra la configuración cargada. Si falta
`a2ensite` o la recarga no llegó a ejecutarse, el sitio nuevo sigue inactivo.

### El certificado es de otro dominio

El navegador y el certificado deben usar el mismo nombre. Comprueba el registro DNS, el `ServerName` o
`server_name` y el nombre de host en **Dominios**. Un certificado de otro nombre no se arregla copiando el
bloque; pide el certificado para el nombre correcto con `certbot --apache -d <nombre-de-host>` o
`certbot --nginx -d <nombre-de-host>`.

### Los WebSockets o las actualizaciones en vivo no funcionan

Comprueba que los módulos están activos (`sudo a2enmod proxy proxy_http proxy_wstunnel headers rewrite`) y
que el bloque del sitio es el del panel. El bloque de Apache actualiza las peticiones WebSocket; nginx
reenvía las cabeceras `Upgrade` y `Connection`. Un proxy o CDN delante del servidor también debe permitir
la actualización.

### No hay bloque de configuración

La app todavía no tiene puerto del anfitrión, lo que normalmente significa que el primer despliegue no ha
terminado correctamente. Abre la app y comprueba **Puerto del anfitrión** y **Despliegues**. Si el servidor
está en modo gestionado, los bloques externos no se aplican; usa
[apps.md](apps.md#proxy-gestionado-caddy).
