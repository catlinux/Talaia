# Instalación

[Read in English](../en/installation.md) · [Volver al README](../../README.es.md)

Talaia tiene dos partes: el **núcleo** (panel, API y base de datos), que corre en Docker en una máquina, y un
**agente** en cada servidor que quieras gestionar.

> **Estado.** El camino manual (apartado 2, «Sin el script») se ejecutó de principio a fin en una máquina
> Debian 13 real. El instalador del núcleo de una sola orden y la descarga del agente desde el núcleo se
> comprobaron por piezas (sintaxis del script, verificación de la huella de la CA y descarga del paquete contra
> un servidor TLS real, tests de Go), pero todavía no de principio a fin en una máquina limpia. Si un paso no
> coincide con lo que ves, abre una incidencia.

## 1. Requisitos

- **Máquina del núcleo:** Debian 13 (o Ubuntu) con Docker Engine y el plugin Docker Compose, y `git`.
- **Servidores a gestionar:** Debian o Ubuntu con systemd, `curl` y acceso root (`sudo`).
- Un nombre o una IP desde la que los servidores y tu navegador lleguen al núcleo, y dos puertos TCP libres para
  él (8443 por defecto); los agentes usan un segundo puerto (8444 por defecto).
- Una aplicación de autenticación que lea códigos TOTP (Google Authenticator, Aegis, 2FAS, Bitwarden…).

## 2. Instalar el núcleo

```bash
git clone https://github.com/catlinux/Talaia.git
cd Talaia
packaging/core/install.sh --hostname 192.0.2.10 --username admin
```

Cambia `192.0.2.10` por la dirección desde la que se llegará al panel (en un terminal la pregunta si omites
`--hostname`). El script:

1. elige los puertos: el primero libre desde 8443 para el panel y desde 8444 para los agentes (lo comprueba con
   `ss`), y nunca el 80 ni el 443 por su cuenta. `--port` y `--agent-port` fuerzan un puerto; si está ocupado,
   el script se detiene;
2. escribe `packaging/core/.env` con una contraseña aleatoria de base de datos y esos puertos (un `.env`
   existente nunca se sustituye, puertos incluidos);
3. instala el ayudante de actualizaciones `talaia-hostd` con `sudo` (véase la sección 5) y luego construye la
   imagen y arranca la pila con `docker compose up -d --build`. La primera construcción tarda
   unos minutos;
4. espera a que el núcleo responda;
5. crea el administrador y te pide la contraseña dos veces (mínimo 12 caracteres).

Con `--acme ovh` (o contestando que sí cuando lo pregunta, en un terminal) prepara además un certificado de
confianza para el panel por ACME DNS-01: las credenciales de OVH se leen de `OVH_ENDPOINT`,
`OVH_APPLICATION_KEY`, `OVH_APPLICATION_SECRET` y `OVH_CONSUMER_KEY` o se piden sin eco, y se guardan en
`packaging/core/.env.acme` (modo 0600, ignorado por git). `--acme-email` fija el correo de contacto. Véase la sección 6.

Se puede volver a ejecutar sin riesgo. Si tu usuario no puede hablar con Docker, usa `sudo docker` por su cuenta.

### Sin el script

```bash
git clone https://github.com/catlinux/Talaia.git
cd Talaia/packaging/core
cp .env.example .env
chmod 600 .env
```

Edita `.env`: pon `POSTGRES_PASSWORD` (genérala con `openssl rand -hex 32`; tiene que ser hexadecimal),
`TALAIA_PUBLIC_HOSTNAMES` (la dirección del núcleo) y los puertos `TALAIA_PANEL_PORT` y `TALAIA_AGENT_PORT`. Después:

```bash
sudo docker compose up -d --build
sudo docker compose ps
sudo docker compose run --rm core user create --username admin
```

`ps` debe mostrar `db` como `healthy`, `volume-init` como `Exited (0)` y `core` como `Up`.

## 3. Primer acceso

1. Abre `https://<dirección>:<puerto>` en el navegador. Avisará de que la conexión no es segura, porque el
   certificado lo firma la CA propia de Talaia. Acepta la excepción, o confía en la CA una vez (ver abajo).
2. Entra con el nombre y la contraseña del administrador.
3. El panel te pide configurar el segundo factor. **Escanea el código QR** con tu aplicación de autenticación
   (o escribe la clave secreta a mano). **No recargues la página**: cada vez que se abre esa pantalla se crea
   una clave nueva y la de tu aplicación deja de valer.
4. Escribe el código de seis cifras de la aplicación. El panel muestra tus **códigos de recuperación**:
   guárdalos en un sitio seguro, solo se muestran una vez.

**Confiar en la CA (quita el aviso del navegador).** Descarga `https://<dirección>:<puerto>/ca.pem`, guárdalo
como `talaia-ca.crt` e instálalo como autoridad raíz de confianza (en Windows: doble clic → Instalar
certificado → Entidades de certificación raíz de confianza). Hazlo solo en tus equipos: quien tenga la clave de
la CA (está en el volumen `core-data`) podría suplantar sitios para ese equipo.

## 4. Añadir un servidor

En el panel, abre **Flota** y pulsa **Añadir servidor**: escribe un nombre, pulsa **Crear token** y copia la orden
que muestra. El token se crea para ese nombre y la orden es la que se describe más abajo.

Desde la consola de la máquina del núcleo se puede hacer lo mismo:

```bash
cd Talaia/packaging/core
sudo docker compose run --rm core token create --name mi-servidor
```

Imprime el token, la huella de la CA y una orden lista para pegar:

```bash
f=$(mktemp) && curl -fsSk https://192.0.2.10:8444/install.sh -o "$f" && echo "<sha256>  $f" | sha256sum -c - && sudo bash "$f" --url https://192.0.2.10:8444 --token … --ca-fingerprint …; rm -f "$f"
```

Ejecuta esa línea **en el servidor que quieres añadir**, en menos de 15 minutos. El token vale una sola vez:
crea otro para el siguiente servidor. El instalador:

- descarga la CA del núcleo y comprueba su huella con `--ca-fingerprint` (el ancla de confianza);
- descarga el paquete del agente desde el núcleo por una conexión que solo confía en esa CA, y lo instala;
- escribe `/etc/talaia/agent.toml` y arranca `talaia-agent`, que se da de alta con el token y recibe su
  propio certificado.

La orden descarga `install.sh` antes de que el servidor confíe en la CA del núcleo, así que no puede verificar la
conexión (`-k`). En su lugar lleva el SHA-256 del script, copiado desde tu panel o tu consola, y se niega a ejecutar
un script que no coincida. También puedes descargarlo antes, leerlo y ejecutarlo desde disco. Con `--deb <ruta>` se instala un
paquete que hayas construido tú (`scripts/build-deb.sh`).

Comprueba el resultado en el servidor:

```bash
sudo systemctl status talaia-agent --no-pager
sudo journalctl -u talaia-agent -n 20 --no-pager
```

El registro debe decir `enrolled with the core` y `connected to core`. El servidor aparece entonces en la
pantalla **Flota** del panel con su CPU, memoria, disco y contenedores.

## 5. Día a día

- **Actualizar el núcleo desde el panel:** la pantalla **Ajustes** muestra la versión en marcha y el commit más
  reciente de `main` con el CI en verde, con los commits que recibirías. **Actualizar** pide confirmación y pasa el
  trabajo al ayudante del host (más abajo); el panel deja de responder unos minutos y sigue el progreso solo.
- **Actualizar el núcleo a mano:** `git pull` y, en `packaging/core`,
  `sudo TALAIA_GIT_COMMIT=$(git rev-parse HEAD) docker compose up -d --build` (sin la variable, el panel muestra la
  versión como desconocida).
- **Conserva los volúmenes:** `db-data` (base de datos) y `core-data` (la CA y la clave que cifra el segundo
  factor). Sin `core-data`, todos los agentes tendrían que darse de alta de nuevo.
- **Has perdido la aplicación o los códigos:** `sudo docker compose run --rm core user reset-2fa` borra el
  segundo factor y cierra todas las sesiones; en el siguiente acceso se vuelve a configurar.
- **Actualizar los agentes:** tras actualizar el núcleo, su imagen trae el paquete nuevo del agente. En la máquina
  del núcleo, `sudo docker compose run --rm -T core server upgrade-command` imprime una orden; ejecútala en cada
  servidor. Comprueba el script igual que la orden de instalación, reinstala el paquete y reinicia el agente,
  conservando su configuración e identidad (no hace falta token).
- **Quitar un servidor:** en la máquina del núcleo, `sudo docker compose run --rm -T core server list` imprime el
  id, el nombre y el estado de cada servidor, y `sudo docker compose run --rm -T core server revoke <id>` lo revoca:
  el núcleo deja de aceptar su certificado. Después, en el propio servidor, `sudo apt purge talaia-agent` quita el
  agente y su configuración.

### Cómo funcionan las actualizaciones desde el panel

El núcleo corre en un contenedor de solo lectura sin el socket de Docker, así que no puede actualizarse solo.
`install.sh` instala en el host un pequeño ayudante que corre como root, **`talaia-hostd`** (se omite con
`--no-hostd`):

- El núcleo solo escribe una petición, `{"target":"<commit>"}`, en `/var/lib/talaia-host/requests/` (el único
  directorio donde puede escribir ahí) y lee `/var/lib/talaia-host/status.json`.
- `talaia-update.path` ve la petición y arranca `talaia-update.service`, que ejecuta
  `/usr/local/sbin/talaia-hostd update`. Saca la petición del alcance del núcleo y solo acepta un commit de 40
  caracteres que esté en `origin/main` y sea más nuevo que el que corre; cualquier otra rama, ruta o campo se rechaza.
- Hace una copia de la base de datos en `/var/lib/talaia-hostd/backups` (las tres últimas, legibles solo por root),
  avanza el clon (git corre como el dueño del clon, no como root), reconstruye y espera hasta tres minutos a
  `/healthz`.
- Si el núcleo nuevo no responde, vuelve al commit anterior, restaura la copia en una sola transacción y reconstruye
  esa versión. El panel muestra el resultado y las últimas líneas del registro; el registro completo está en
  `sudo journalctl -u talaia-update`. Lo que el panel registró entre la copia y la vuelta atrás (auditoría,
  métricas) se pierde.

Su configuración está en `/etc/talaia/hostd.conf` (`REPO_DIR`, `REPO_USER`). El clon debe seguir en `main` sin
cambios locales. Quien sea dueño del clon puede cambiar lo que se construye como root, así que déjalo en una cuenta
de administración.

## 6. El núcleo en un servidor público

Los agentes se conectan **hacia fuera** con el núcleo, así que el mejor sitio para este es una máquina con IP
pública (un VPS): los servidores detrás del router de casa o de cualquier NAT se dan de alta y siguen conectados sin
VPN ni puerto abierto por su lado.

- **No necesita los puertos 80 ni 443.** El instalador elige puertos libres (8443 y 8444 por defecto), de modo que
  un VPS que ya tenga un servidor web o un proxy inverso sigue funcionando. Abre los dos puertos elegidos en el
  cortafuegos del VPS (quedan escritos en `packaging/core/.env`) y apunta un registro DNS `A` al VPS.
- **Instala con el nombre público:** `packaging/core/install.sh --hostname talaia.example.com --username admin`.
- **El panel queda entonces en internet.** Lo protegen la contraseña más el segundo factor, el límite de intentos y
  el bloqueo tras 5 accesos fallidos (véase la sección 7). Usa una contraseña larga y guarda bien la aplicación de
  autenticación.

### Un certificado en el que confían los navegadores (ACME DNS-01)

Con la CA interna el navegador avisa de que el panel «no es seguro». Si la zona DNS de tu dominio está en OVH, el
núcleo puede obtener solo un certificado de Let's Encrypt. DNS-01 demuestra el nombre con un registro DNS, así que
funciona en cualquier puerto y no necesita servidor web. Hace falta un nombre DNS en `--hostname` (una dirección IP
no puede tenerlo).

1. Crea una aplicación de la API de OVH y una clave de consumidor siguiendo la
   [guía de OVH](https://docs.ovh.com/gb/en/customer/first-steps-with-ovh-api/). Da a la clave de consumidor
   derechos solo sobre tu zona (sustituye `example.com` por ella): son las tres llamadas que hace lego, la
   biblioteca ACME que usa el núcleo. La documentación de lego propone `/domain/zone/*`, que dejaría a la clave
   cambiar todas las zonas de la cuenta si alguien llegara a controlar el núcleo.

   ```json
   { "accessRules": [
       { "method": "POST", "path": "/domain/zone/example.com/record" },
       { "method": "POST", "path": "/domain/zone/example.com/refresh" },
       { "method": "DELETE", "path": "/domain/zone/example.com/record/*" }
   ] }
   ```

2. Instala con el proveedor: `packaging/core/install.sh --hostname talaia.example.com --acme ovh --acme-email tu@example.com`
   (el correo es opcional). Las credenciales van a `packaging/core/.env.acme` (modo 0600) y el núcleo las lee como
   `OVH_ENDPOINT`, `OVH_APPLICATION_KEY`, `OVH_APPLICATION_SECRET` y `OVH_CONSUMER_KEY`.
3. Hasta que el certificado esté listo, el panel responde con el de la CA interna; después pasa al de confianza sin
   reiniciar. El núcleo registra `ACME certificate ready`. La cuenta y el certificado se guardan en el volumen
   `core-data` (`/var/lib/talaia/acme`) y se renuevan 30 días antes de caducar. Si
   se abre por dirección IP, el panel sigue respondiendo con el certificado de la CA interna.

El **puerto de los agentes usa siempre la CA interna**, nunca el certificado público: los agentes fijan la huella de
la CA, así que quien se haga con tu DNS no puede hacerse pasar por el núcleo y mandar órdenes a tus servidores.

> Escrito a partir del código y de sus pruebas con un servidor ACME simulado; todavía sin probar contra Let's
> Encrypt ni OVH.

### Alternativa: una VPN privada

Si prefieres no exponer el panel, deja el núcleo en una red privada y llega a él por una VPN como Tailscale:
instálala en la máquina del núcleo y en tus dispositivos (sigue su propia documentación), añade la dirección o el
nombre de VPN a `TALAIA_PUBLIC_HOSTNAMES` en `packaging/core/.env`, separada por una coma, ejecuta
`sudo docker compose up -d` en `packaging/core` y abre `https://<dirección-vpn>:<puerto>`. El núcleo firma su
certificado para todos los nombres de la lista. Escrito a partir del código y sin probar en una VPN real.

## 7. Problemas frecuentes

| Síntoma | Causa y solución |
|---|---|
| El navegador dice «no es seguro» | Es lo esperado con la CA propia. Acepta la excepción o confía en `ca.pem`. |
| Se rechaza el código de seis cifras | Espera al siguiente código y vuelve a probar. Tras 5 intentos erróneos se cierra la sesión, y 5 fallos bloquean al usuario 15 minutos. Comprueba que la hora de ambos dispositivos es correcta. |
| El panel pide un código de recuperación que no tienes | `user reset-2fa` (arriba). |
| `install.sh`: «does not match --ca-fingerprint» | La dirección responde con otra CA. Revisa `--url` y copia de nuevo la huella que imprime `token create`. |
| El agente no se da de alta | El token caducó (15 minutos) o ya se usó: crea otro. |
| `./install.sh: command not found` | Ejecútalo con `bash install.sh …`, o dale permiso con `chmod +x`. |
