# Certificado de confianza con dos registros DNS

[Read in English](../en/dns-delegation.md) · [Llegir en català](../ca/dns-delegation.md) · [Volver a la instalación](instalacion.md#un-certificado-en-el-que-confían-los-navegadores-acme-dns-01)

Es la forma recomendada de dar al panel un certificado en el que confían los navegadores. Creas dos registros en
la zona DNS de tu dominio, en la empresa que la tenga, y el instalador hace el resto. Sin claves de API, sin
ninguna cuenta de tu proveedor de DNS en el servidor y sin usar los puertos 80 ni 443: un servidor web como
Apache que ya funcione en la máquina no se toca.

## Cómo funciona

Para emitir un certificado para `talaia.example.com`, Let's Encrypt pide un registro `TXT` en
`_acme-challenge.talaia.example.com`. El registro `NS` de abajo delega en tu servidor exactamente ese nombre, y
nada más. Talaia lleva ahí un servidor DNS mínimo (el servicio `acme-dns`) que solo contesta ese nombre y solo
mientras se emite o se renueva un certificado.

## 1. Crea los dos registros

En la zona DNS de `example.com` (sustituye por tu dominio y el nombre que hayas elegido):

| Tipo | Nombre (subdominio) | Valor (destino) |
|---|---|---|
| `A` | `talaia` | la dirección IPv4 pública de tu servidor |
| `NS` | `_acme-challenge.talaia` | `talaia.example.com` |

- **No** crees un registro `AAAA` para `talaia`: por ahora el servidor del desafío solo contesta por IPv4.
- El instalador imprime esta tabla con tus valores y la dirección de tu servidor.

### En OVH

1. En el panel de control de OVHcloud, abre **Nombres de dominio** (*Domain names*) y elige tu dominio.
2. Abre la pestaña **Zona DNS** (*DNS zone*) y pulsa **Añadir una entrada** (*Add an entry*).
3. Para el registro `A`: elige **A**, escribe `talaia` en **Subdominio** (*Subdomain*) y la IP del servidor
   como destino. Confirma con **Añadir**. Si ya lo creaste, comprueba que la dirección es la correcta.
4. Para el registro `NS`: otra vez **Añadir una entrada**, elige **NS**, escribe `_acme-challenge.talaia` en
   **Subdominio** y `talaia.example.com` en **Destino (servidor DNS)** (*Target (DNS server)*). Si el formulario
   lo acepta, termina el destino con un punto (`talaia.example.com.`); si no, déjalo sin él. Confirma con
   **Añadir**.

### En otros proveedores

Los mismos dos registros existen en cualquier proveedor de DNS. Busca «añadir registro» en la zona de tu
dominio; el campo `Nombre`/`Host` lleva el subdominio (`talaia`, `_acme-challenge.talaia`) y el campo
`Valor`/`Destino`, la dirección o el nombre. Algunos proveedores piden el nombre completo
(`_acme-challenge.talaia.example.com`).

Los cambios suelen verse en pocos minutos; OVH avisa de que pueden tardar más en llegar a todo el mundo.

## 2. Abre el puerto 53

El servidor del desafío escucha en el puerto 53 UDP **y** TCP de la dirección pública. El instalador:

- pregunta antes de abrirlo en `ufw` o `firewalld` si alguno está activo, y solo lo hace si dices que sí;
- te recuerda abrirlo en el cortafuegos de tu proveedor (panel de control, grupo de seguridad), que él no puede
  tocar.

Los puertos del panel y de los agentes (8443 y 8444 por defecto) tienen que estar abiertos en los mismos sitios.

## 3. Deja que el instalador lo compruebe

Cuando los registros existan y el puerto esté abierto, pulsa Enter en el instalador. Ejecuta
`talaia-core acme check-delegation`, que imprime una línea por comprobación:

| Línea | Qué significa y cómo arreglarlo |
|---|---|
| `a-record: ok` | El nombre apunta a este servidor. |
| `a-record: missing` / `other` | Crea o corrige el registro `A` (la línea dice a dónde apunta ahora). |
| `aaaa-record: present` | Borra el registro `AAAA` del nombre. |
| `delegation: ok` | El registro `NS` existe en tu zona y apunta al nombre del panel. |
| `delegation: missing` | El registro `NS` todavía no está, o tiene otro nombre. Créalo como en el paso 1. |
| `delegation: other <nombre>` | El registro `NS` apunta a otro sitio. Un destino como `talaia.example.com.example.com.` significa que el proveedor le añadió tu dominio: escribe el destino con punto final, o sin el dominio, según pida el formulario. |
| `dns-server: ok` | Tu servidor contesta en el puerto 53 a través de la delegación. |
| `dns-server: unreachable` | El puerto 53 está cerrado en algún sitio: en `ufw`/`firewalld` o en el cortafuegos del proveedor. Abre el 53 UDP y TCP. |
| `dns-server: not delegated yet` | Los resolvedores todavía no ven el registro `NS`. Espera unos minutos y vuelve a comprobar. |

Cuando todo está en `ok`, el instalador pide un certificado al entorno de pruebas de Let's Encrypt (*staging*).
Eso demuestra toda la cadena desde internet sin gastar los límites del servicio real. Después pide el de verdad
y dice `ACME certificate: obtained`.

Si algo no funciona, puedes seguir sin certificado de confianza (el panel usa el de Talaia) y ejecutar
`packaging/core/install.sh --acme delegation` más tarde para terminar.

## Seguridad

- Nada en el servidor puede cambiar tu zona DNS: no hay claves del proveedor. El registro `NS` solo permite a
  este servidor contestar `_acme-challenge.talaia.example.com`, que solo sirve para certificados de
  `talaia.example.com`.
- El servidor DNS corre en su propio contenedor, de solo lectura, sin la base de datos, los secretos ni los datos
  del núcleo; solo lee los valores pendientes del desafío, que de todos modos son públicos. Contesta ese nombre y
  rechaza todo lo demás, nunca resuelve otros nombres para nadie (sin recursión), mantiene sus respuestas
  pequeñas y descarta las consultas que pasan de un ritmo por red de origen, así que no sirve para amplificar
  ataques.
- Opcional: un registro `CAA` en `talaia` con el valor `0 issue "letsencrypt.org"` indica a cualquier otra
  autoridad de certificación que no emita para ese nombre, si tu proveedor admite registros `CAA`.
- Más en [seguridad.md](seguridad.md).

## Deshacerlo

En `packaging/core`:

```bash
sudo docker compose --profile acme-dns rm -sf acme-dns
```

luego pon `TALAIA_ACME_PROVIDER=` (vacío) y quita la línea `COMPOSE_PROFILES=acme-dns` de `.env`, ejecuta
`sudo docker compose up -d` y borra el registro `NS`. El panel vuelve al certificado de la CA propia de Talaia.
