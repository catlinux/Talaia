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
- Un nombre o una IP desde la que los servidores y tu navegador lleguen al núcleo, y un puerto TCP libre para
  él (443 por defecto, o por ejemplo 8443).
- Una aplicación de autenticación que lea códigos TOTP (Google Authenticator, Aegis, 2FAS, Bitwarden…).

## 2. Instalar el núcleo

```bash
git clone https://github.com/catlinux/Talaia.git
cd Talaia
packaging/core/install.sh --hostname 192.0.2.10 --port 8443 --username admin
```

Cambia `192.0.2.10` por la dirección desde la que se llegará al panel. El script:

1. escribe `packaging/core/.env` con una contraseña aleatoria de base de datos (un `.env` existente nunca se
   sustituye);
2. construye la imagen y arranca la pila con `docker compose up -d --build`. La primera construcción tarda
   unos minutos;
3. espera a que el núcleo responda;
4. crea el administrador y te pide la contraseña dos veces (mínimo 12 caracteres).

Se puede volver a ejecutar sin riesgo. Si tu usuario no puede hablar con Docker, usa `sudo docker` por su cuenta.

### Sin el script

```bash
git clone https://github.com/catlinux/Talaia.git
cd Talaia/packaging/core
cp .env.example .env
chmod 600 .env
```

Edita `.env`: pon `POSTGRES_PASSWORD` (genérala con `openssl rand -hex 32`; tiene que ser hexadecimal),
`TALAIA_PUBLIC_HOSTNAMES` (la dirección del núcleo) y `TALAIA_PORT`. Después:

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
curl -fsSk https://192.0.2.10:8443/install.sh | sudo bash -s -- --url https://192.0.2.10:8443 --token … --ca-fingerprint …
```

Ejecuta esa línea **en el servidor que quieres añadir**, en menos de 15 minutos. El token vale una sola vez:
crea otro para el siguiente servidor. El instalador:

- descarga la CA del núcleo y comprueba su huella con `--ca-fingerprint` (el ancla de confianza);
- descarga el paquete del agente desde el núcleo por una conexión que solo confía en esa CA, y lo instala;
- escribe `/etc/talaia/agent.toml` y arranca `talaia-agent`, que se da de alta con el token y recibe su
  propio certificado.

El propio `install.sh` es el único fichero que se descarga sin verificar (`-k`), como en cualquier
`curl | bash`. Si prefieres, descárgalo antes, léelo y ejecútalo desde disco. Con `--deb <ruta>` se instala un
paquete que hayas construido tú (`scripts/build-deb.sh`).

Comprueba el resultado en el servidor:

```bash
sudo systemctl status talaia-agent --no-pager
sudo journalctl -u talaia-agent -n 20 --no-pager
```

El registro debe decir `enrolled with the core` y `connected to core`. El servidor aparece entonces en la
pantalla **Flota** del panel con su CPU, memoria, disco y contenedores.

## 5. Día a día

- **Actualizar el núcleo:** `git pull` y, en `packaging/core`, `sudo docker compose up -d --build`.
- **Conserva los volúmenes:** `db-data` (base de datos) y `core-data` (la CA y la clave que cifra el segundo
  factor). Sin `core-data`, todos los agentes tendrían que darse de alta de nuevo.
- **Has perdido la aplicación o los códigos:** `sudo docker compose run --rm core user reset-2fa` borra el
  segundo factor y cierra todas las sesiones; en el siguiente acceso se vuelve a configurar.
- **Quitar un servidor:** en la máquina del núcleo, `sudo docker compose run --rm -T core server list` imprime el
  id, el nombre y el estado de cada servidor, y `sudo docker compose run --rm -T core server revoke <id>` lo revoca:
  el núcleo deja de aceptar su certificado. Después, en el propio servidor, `sudo apt purge talaia-agent` quita el
  agente y su configuración.

## 6. Problemas frecuentes

| Síntoma | Causa y solución |
|---|---|
| El navegador dice «no es seguro» | Es lo esperado con la CA propia. Acepta la excepción o confía en `ca.pem`. |
| Se rechaza el código de seis cifras | Espera al siguiente código y vuelve a probar. Tras 5 intentos erróneos se cierra la sesión, y 5 fallos bloquean al usuario 15 minutos. Comprueba que la hora de ambos dispositivos es correcta. |
| El panel pide un código de recuperación que no tienes | `user reset-2fa` (arriba). |
| `install.sh`: «does not match --ca-fingerprint» | La dirección responde con otra CA. Revisa `--url` y copia de nuevo la huella que imprime `token create`. |
| El agente no se da de alta | El token caducó (15 minutos) o ya se usó: crea otro. |
| `./install.sh: command not found` | Ejecútalo con `bash install.sh …`, o dale permiso con `chmod +x`. |
