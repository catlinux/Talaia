# Certificado del panel sin claves de proveedor: delegación DNS del desafío ACME

Estado: **diseño de Opus (2026-10-21).** Pedido por el mantenedor: que el instalador consiga el certificado de
confianza sin pasar por la web de OVH, creando solo registros en la zona DNS. Sustituye a la clave de OVH como
método principal; la clave de OVH (`ovh`) y la CA propia quedan como alternativas.

## Idea

El usuario crea en su zona DNS (cualquier proveedor) dos registros:

| Tipo | Nombre | Valor |
|---|---|---|
| `A` | `talaia` | IP pública del VPS |
| `NS` | `_acme-challenge.talaia` | `talaia.example.com.` |

El `NS` delega en el VPS solo el nombre del desafío. Talaia lleva un servidor DNS autoritativo mínimo que contesta
el `TXT` de ese nombre mientras dura una emisión. Let's Encrypt lo consulta, valida y emite. No hay credenciales
de proveedor en el servidor.

## Decisiones

1. **Proceso aparte.** `talaia-core dns` (misma imagen) corre en su propio servicio de compose `acme-dns`: sin base
   de datos, sin `secrets.key`, sin el volumen `core-data`; solo lee un fichero de desafíos en un volumen propio
   (`acme-challenge`, escrito por el core, montado de solo lectura). Contenedor de solo lectura, sin capacidades,
   uid 65532, `mem_limit` y `pids_limit`. Escucha en `:5353` dentro del contenedor; compose publica
   `${TALAIA_DNS_ADDRESS}:53` UDP y TCP (la IP pública concreta: `systemd-resolved` ocupa `127.0.0.53:53` y no
   permite enlazar `0.0.0.0:53`). Se activa con el perfil de compose `acme-dns` (`COMPOSE_PROFILES` en `.env`).
2. **Qué contesta** (`core/internal/acmedns`, con `github.com/miekg/dns`, que ya trae lego):
   - Autoritativo solo para `_acme-challenge.<host>` de cada nombre DNS de `TALAIA_PUBLIC_HOSTNAMES`.
   - `TXT` → los valores vigentes del fichero (0..n; TTL 10 s). `SOA` y `NS` del ápice → sintetizados (NS = el
     host). Cualquier otro tipo del ápice → NODATA con el SOA (mínimo 10 s). Nombres fuera → `REFUSED`, sin
     respuesta extra. Nunca recursión (RA=0). Sin `ANY`: NODATA (RFC 8482 permite respuesta mínima).
   - Antiamplificación: respuestas pequeñas por construcción; tamaño UDP máx. 1232 con EDNS y truncado (TC) si no
     cabe; límite por origen (/24 IPv4, /56 IPv6) de 20 consultas/s con ráfaga de 40, y lo que exceda se descarta
     sin responder. TCP: 64 conexiones, 5 s de lectura/escritura, una consulta por conexión.
   - Valores con caducidad (30 min) en el fichero: uno olvidado no se sirve para siempre.
3. **Proveedor de lego `delegation`** (`core/internal/acme`): `Present` escribe el valor en el fichero de desafíos
   (atómico, 0644) y `CleanUp` lo quita. Comprobación previa propia (`WrapPreCheck`): consulta el servicio
   `acme-dns` por la red interna de compose (`acme-dns:5353`) hasta ver el valor; no depende de que el VPS se
   alcance a sí mismo por su IP pública ni de la propagación (la delegación es estática).
4. **Almacenamiento por directorio ACME** (`<data_dir>/acme/<host del directorio>/`): cuenta y certificado de
   *staging* y de producción no se mezclan. Corrige además que una cuenta registrada en un directorio se
   reutilizara en otro.
5. **Prueba de extremo a extremo antes del certificado real:** el instalador arranca con el directorio de
   *staging* de Let's Encrypt (sin límites que gastar), espera `ACME certificate ready` y solo entonces pasa a
   producción. Demuestra delegación, puerto 53 abierto y servidor DNS desde fuera.
6. **Comprobación desde el instalador** (`talaia-core acme check-delegation`, ejecutado con `compose run`):
   consulta resolvedores públicos (1.1.1.1, 8.8.8.8, 9.9.9.9) por el `A` del host y el `NS` del nombre del desafío,
   y dice exactamente qué falta o qué está mal (IP distinta, NS a otro nombre, registro aún no visible).
7. **Cortafuegos:** el instalador pide permiso para abrir 53 UDP+TCP (y los puertos del panel y agentes) en `ufw`
   o `firewalld` y lo hace si se acepta; para el del proveedor, dice qué abrir y espera. Los puertos que publica
   Docker no los filtra `ufw`: lo que cuenta es el cortafuegos del proveedor.
8. **IPv6:** si el host tiene `AAAA`, Let's Encrypt puede preguntar por IPv6; en esta versión el servicio DNS solo
   se publica en IPv4 y el instalador pide no crear `AAAA` para el nombre (o avisa si existe).

## Pasos

| # | Paso | Archivos | Hecho cuando | Ejecutor |
|---|---|---|---|---|
| 1 ✅ | Servidor DNS, proveedor `delegation`, almacenamiento por directorio, `talaia-core dns` y `acme check-delegation`, servicio de compose con perfil | `core/internal/acmedns` (nuevo), `core/internal/acme`, `core/internal/config`, `core/cmd/talaia-core`, `packaging/core/compose.yaml`, `.env.example` | pruebas: respuestas (TXT, SOA, NS, NODATA, REFUSED, TC), límite por origen, caducidad, recarga del fichero, proveedor y precomprobación, almacenamiento por directorio | **Opus** |
| 2 ✅ | Instalador: asistente de delegación (IP pública, puerto 53, registros a crear, permiso para abrir puertos, espera y reintento, comprobación, *staging* → producción, alternativas OVH o CA propia) | `packaging/core/install.sh`, `test-install.sh`, CI | pruebas con dobles; CI `core stack` arranca `acme-dns` y le pregunta | Sonnet, alto |
| 3 ✅ | Guías EN/ES/CA: crear los dos registros (con los pasos de OVH), seguridad del puerto 53; instalación y seguridad al día | `docs/*/dns-delegation.md` (nuevo), `docs/*/installation*`, `docs/*/security*`/`seguridad` | revisadas contra lo construido | Sonnet, bajo |
| 4 | Revisión de seguridad de 1–3 | — | hallazgos corregidos | **Opus** |
| 5 | Prueba real en el VPS con el mantenedor | — | panel con candado sin claves de OVH | sesión con el mantenedor |
