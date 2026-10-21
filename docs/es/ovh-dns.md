# Certificado de confianza con OVH: obtener la clave de la API

[Read in English](../en/ovh-dns.md) · [Llegir en català](../ca/ovh-dns.md) · [Volver a la instalación](instalacion.md#un-certificado-en-el-que-confían-los-navegadores-acme-dns-01)

Sin esto, el panel funciona, pero el navegador avisa de que la conexión «no es segura», porque Talaia firma su
certificado con su propia autoridad. Si el DNS de tu dominio está en OVH, Talaia puede conseguir solo un
certificado gratuito de Let's Encrypt. Para eso necesita una clave de OVH autorizada a cambiar **solo tu zona
DNS**. Esta guía la crea paso a paso y lleva unos cinco minutos.

**Cómo funciona.** Let's Encrypt pide a Talaia que demuestre que el dominio es tuyo publicando un registro `TXT`
temporal en tu zona DNS. Talaia lo hace por la API de OVH con esta clave y lo borra después. Por eso no hace falta
que nada escuche en los puertos 80 o 443, y un servidor con Apache u otro servidor web no se ve afectado. La clave
se queda en tu servidor, en `packaging/core/.env.acme` (solo legible por root).

## Antes de empezar

- El dominio debe usar los **servidores de nombres de OVH** (Web Cloud → Nombres de dominio → tu dominio →
  *Servidores DNS*). Si usa otros, crea la clave en la empresa que aloja tu DNS; por ahora Talaia solo admite OVH, y
  sin ella puedes usar el panel con su certificado propio.
- Crea, en la zona DNS, el registro que apunta el nombre elegido a tu servidor (Web Cloud → Nombres de dominio →
  tu dominio → *Zona DNS* → *Añadir una entrada*): tipo **A** (y **AAAA** si el servidor tiene IPv6), el subdominio
  (por ejemplo `talaia`) y la dirección IP pública de tu servidor. El instalador avisa si el nombre todavía no
  resuelve. **No** hace falta configurar nada en Apache ni en el servidor.
- Averigua tu **zona**: es el dominio registrado, no el subdominio. Para `talaia.example.com` la zona es
  `example.com`.

## 1. Abre la página de creación de claves

El instalador imprime un enlace ya preparado para tu zona. Si prefieres construirlo tú, sustituye `example.com` por
tu zona:

- OVH Europa (cuentas de ovh.com en Europa y la mayor parte del mundo):

  ```text
  https://api.eu.ovhcloud.com/createToken/?POST=/domain/zone/example.com/record&POST=/domain/zone/example.com/refresh&DELETE=/domain/zone/example.com/record/*
  ```

- OVH Canadá:

  ```text
  https://ca.api.ovh.com/createToken/?POST=/domain/zone/example.com/record&POST=/domain/zone/example.com/refresh&DELETE=/domain/zone/example.com/record/*
  ```

Inicia sesión con tu cuenta de OVH cuando lo pida.

## 2. Rellena el formulario

| Campo | Qué poner |
|---|---|
| Nombre y descripción de la aplicación | Lo que reconozcas más adelante, por ejemplo `Talaia` y `Certificado del panel de Talaia`. |
| Validez | La más larga, **ilimitada** si se ofrece. Talaia renueva el certificado cada 60 días; si la clave caduca, la renovación falla y el panel vuelve al aviso. |
| Derechos | Estos tres y nada más. Deberían aparecer ya rellenados desde el enlace; si la lista está vacía o incompleta, añade los que falten con el botón **+**. |

Los tres derechos, con tu zona en lugar de `example.com`:

| Método | Ruta |
|---|---|
| `POST` | `/domain/zone/example.com/record` |
| `POST` | `/domain/zone/example.com/refresh` |
| `DELETE` | `/domain/zone/example.com/record/*` |

Permiten añadir y borrar registros en esa zona, y nada de tu cuenta ni de otras zonas; son las únicas llamadas que
hace Talaia. **No** uses `*` para todos los métodos y rutas: quien se hiciera con esa clave controlaría toda tu
cuenta de OVH.

Crea las claves.

## 3. Copia los tres valores

OVH muestra tres valores. **Cópialos ahora**, no se pueden volver a mostrar:

| Cómo lo llama OVH | Aspecto | Notas |
|---|---|---|
| Application key | 16 caracteres | El instalador la pide como `OVH application key`. |
| Application secret | 32 caracteres | Secreto. Se pide sin eco. |
| Consumer key | 32 caracteres | Secreto. Se pide sin eco. |

Anota también el **endpoint**: `ovh-eu` si usaste el enlace de Europa, `ovh-ca` para Canadá.

## 4. Dáselos al instalador

Ejecuta el instalador con `--acme ovh` (o contesta que sí cuando lo pregunte) y pega los tres valores cuando los
pida. Los comprueba con OVH **antes** de construir nada y te dice exactamente qué falla:

- la clave no existe o ha caducado;
- no tiene los tres derechos (y cuál falta);
- no se puede llegar a OVH.

Si prefieres no teclearlos, expórtalos antes:

```bash
export OVH_ENDPOINT=ovh-eu OVH_APPLICATION_KEY=… OVH_APPLICATION_SECRET=… OVH_CONSUMER_KEY=…
packaging/core/install.sh --hostname talaia.example.com --acme ovh
```

A los pocos minutos de arrancar, el instalador informa de `ACME certificate: obtained`.

## Si algo falla

| Qué ves | Causa probable y arreglo |
|---|---|
| El instalador dice que la clave no es válida | Un valor está mal escrito o la clave ha caducado. Crea una nueva (paso 2) y vuelve a ejecutar el instalador. |
| Lista un derecho que falta | Crea una clave nueva con los tres derechos de arriba (paso 2) y vuelve a ejecutar el instalador. |
| `ACME certificate: still pending` | Espera unos minutos (el DNS puede tardar) y mira `sudo docker compose logs core \| grep -i acme`. El núcleo sigue intentándolo y, mientras, el panel funciona con su certificado propio. |
| El registro dice que no encuentra la zona o que el nombre no está en OVH | El dominio no usa los servidores de nombres de OVH, o la zona es incorrecta. Revisa *Antes de empezar*. |
| El navegador sigue avisando tras obtener el certificado | Abriste el panel por dirección IP o por un nombre que no está en `--hostname`. Usa el nombre. |

## Cambiar o revocar la clave

- **Cambiarla:** crea una nueva, edita `packaging/core/.env.acme` (cuatro líneas, modo 0600) y ejecuta
  `sudo docker compose up -d` en `packaging/core`.
- **Revocarla** si se filtra o dejas de usar Talaia: en tu cuenta de OVH, en las claves de API del panel de
  control, o con la API (`/me/api/application`). El certificado sigue funcionando hasta que caduca, pero no se puede
  renovar.
