# Installation

[Leer en castellano](../es/instalacion.md) · [Back to README](../../README.md)

Talaia has two parts: the **core** (panel, API and database), which runs in Docker on one machine, and an
**agent** on every server you want to manage.

> **Status.** The manual path (section 2, "Without the script") was run end to end on a real Debian 13
> machine. The one-command core installer and the agent download from the core were checked piece by piece
> (script syntax, CA fingerprint check and package download against a real TLS server, Go tests), but not yet
> end to end on a clean machine. If a step here does not match what you see, please open an issue.

## 1. Requirements

- **Core machine:** Debian 13 (or Ubuntu) with Docker Engine and the Docker Compose plugin, and `git`.
- **Servers to manage:** Debian or Ubuntu with systemd, `curl`, and root access (`sudo`).
- A name or IP address the servers and your browser can reach the core at, and two free TCP ports for it
  (8443 by default); a second port is used by the agents (8444 by default).
- An authenticator app that reads TOTP codes (Google Authenticator, Aegis, 2FAS, Bitwarden…).

## 2. Install the core

```bash
git clone https://github.com/catlinux/Talaia.git
cd Talaia
packaging/core/install.sh --hostname 192.0.2.10 --username admin
```

Replace `192.0.2.10` with the address the panel will be reached at (in a terminal it asks for it when you omit
`--hostname`). The script:

1. chooses the ports: the first free one from 8443 for the panel and from 8444 for the agents (checked with
   `ss`), never 80 or 443 on its own. `--port` and `--agent-port` force a port; if it is busy the script stops;
2. writes `packaging/core/.env` with a random database password and those ports (an existing `.env` is never
   replaced, ports included);
3. builds the image and starts the stack with `docker compose up -d --build`. The first build takes a few
   minutes;
4. waits for the core to answer;
5. creates the administrator, asking for the password twice (at least 12 characters).

With `--acme ovh` (or answering yes when it asks, in a terminal) it also prepares a trusted certificate for the
panel by ACME DNS-01: the OVH credentials are read from `OVH_ENDPOINT`, `OVH_APPLICATION_KEY`,
`OVH_APPLICATION_SECRET` and `OVH_CONSUMER_KEY` or asked for without echo, and are stored in
`packaging/core/.env.acme` (mode 0600, ignored by git). `--acme-email` sets the contact address. See section 6.

It is safe to run again. If your user cannot talk to Docker, it uses `sudo docker` on its own.

### Without the script

```bash
git clone https://github.com/catlinux/Talaia.git
cd Talaia/packaging/core
cp .env.example .env
chmod 600 .env
```

Edit `.env`: set `POSTGRES_PASSWORD` (generate it with `openssl rand -hex 32`; it must be hexadecimal),
`TALAIA_PUBLIC_HOSTNAMES` (the address of the core) and the ports `TALAIA_PANEL_PORT` and `TALAIA_AGENT_PORT`. Then:

```bash
sudo docker compose up -d --build
sudo docker compose ps
sudo docker compose run --rm core user create --username admin
```

`ps` must show `db` as `healthy`, `volume-init` as `Exited (0)` and `core` as `Up`.

## 3. First sign-in

1. Open `https://<address>:<port>` in your browser. It warns that the connection is not secure, because the
   certificate is signed by Talaia's own CA. Accept the exception, or trust the CA once (see below).
2. Sign in with the administrator's name and password.
3. The panel asks you to set up the second factor. **Scan the QR code** with your authenticator app (or type
   the secret key by hand). **Do not reload the page**: every time that screen opens it creates a new key, and
   the one in your app stops working.
4. Type the six-digit code of the app. The panel shows your **recovery codes**: keep them somewhere safe, they
   are shown only once.

**Trusting the CA (removes the browser warning).** Download `https://<address>:<port>/ca.pem`, save it as
`talaia-ca.crt` and install it as a trusted root authority (on Windows: double click → Install certificate →
Trusted Root Certification Authorities). Only do it on your own machines: whoever holds the CA key (it lives in
the `core-data` volume) could impersonate sites for it.

## 4. Add a server

In the panel, open **Fleet** and press **Add server**: type a name, press **Create token** and copy the command it
shows. The token is created for that name and the command is the one described below.

From the console of the core machine you can do the same:

```bash
cd Talaia/packaging/core
sudo docker compose run --rm core token create --name my-server
```

It prints the token, the CA fingerprint and a ready-to-paste command:

```bash
f=$(mktemp) && curl -fsSk https://192.0.2.10:8444/install.sh -o "$f" && echo "<sha256>  $f" | sha256sum -c - && sudo bash "$f" --url https://192.0.2.10:8444 --token … --ca-fingerprint …; rm -f "$f"
```

Run that line **on the server you want to add**, within 15 minutes. The token works once; create another one
for the next server. The installer:

- fetches the core CA and checks its fingerprint against `--ca-fingerprint` (the trust anchor);
- downloads the agent package from the core over a connection that trusts that CA alone, and installs it;
- writes `/etc/talaia/agent.toml` and starts `talaia-agent`, which enrolls with the token and receives its own
  certificate.

The command fetches `install.sh` before the server trusts the core CA, so it cannot verify the connection (`-k`).
Instead it carries the SHA-256 of the script, copied from your panel or console, and refuses to run a script that
does not match. You can also download it first, read it, and run it from disk. Use `--deb <path>` to install a package you built
yourself (`scripts/build-deb.sh`).

Check the result on the server:

```bash
sudo systemctl status talaia-agent --no-pager
sudo journalctl -u talaia-agent -n 20 --no-pager
```

The log must say `enrolled with the core` and `connected to core`. The server then appears in the panel's
**Fleet** screen with its CPU, memory, disk and containers.

## 5. Day to day

- **Update the core:** `git pull`, then in `packaging/core` run `sudo docker compose up -d --build`.
- **See if there is an update:** the **Settings** screen of the panel shows the running version and the newest commit of `main` whose CI is green, with the commits you would get. Applying it from the panel needs the host helper, which is not built yet: for now, update as in the first bullet.
- **Keep the volumes:** `db-data` (database) and `core-data` (the CA and the key that encrypts the second
  factor). Without `core-data` every agent would have to enroll again.
- **Lost the authenticator or the codes:** `sudo docker compose run --rm core user reset-2fa` clears the second
  factor and closes all sessions; the next sign-in sets it up again.
- **Logs:** `sudo docker compose logs core`.
- **Update the agents:** after updating the core, its image carries the new agent package. On the core machine,
  `sudo docker compose run --rm -T core server upgrade-command` prints a command; run it on each server. It checks
  the script like the install command does, reinstalls the package and restarts the agent, keeping its
  configuration and identity (no token needed).
- **Remove a server:** on the core machine, `sudo docker compose run --rm -T core server list` prints each server's
  id, name and state, and `sudo docker compose run --rm -T core server revoke <id>` revokes it: the core stops
  accepting its certificate. Then, on the server itself, `sudo apt purge talaia-agent` removes the agent and its
  configuration.

## 6. Running the core on a public server

The agents connect **outbound** to the core, so the best place for the core is a machine with a public IP address
(a VPS): servers behind a home router or any NAT enroll and stay connected without a VPN or an open port on their
side.

- **It does not need ports 80 or 443.** The installer picks free ports (8443 and 8444 by default), so a VPS that
  already runs a web server or a reverse proxy keeps working. Open the two chosen ports in the firewall of the VPS
  (they are written to `packaging/core/.env`) and point a DNS `A` record at the VPS.
- **Install with the public name:** `packaging/core/install.sh --hostname talaia.example.com --username admin`.
- **The panel is then on the internet.** What protects it is the password plus the second factor, the rate limit
  and the lockout after 5 failed sign-ins (see section 7). Keep the password long and the authenticator safe.

### A certificate browsers trust (ACME DNS-01)

With the internal CA the browser warns that the panel is "not secure". If the DNS zone of your domain is at OVH,
the core can obtain a certificate from Let's Encrypt on its own. DNS-01 proves the name through a DNS record, so
it works on any port and needs no web server. It needs a DNS name in `--hostname` (an IP address cannot get one).

1. Create an OVH API application and a consumer key following the
   [OVH guide](https://docs.ovh.com/gb/en/customer/first-steps-with-ovh-api/). Give the consumer key the minimum
   rights, as lego (the ACME library the core uses) documents:

   ```json
   { "accessRules": [
       { "method": "POST", "path": "/domain/zone/*" },
       { "method": "DELETE", "path": "/domain/zone/*" }
   ] }
   ```

2. Install with the provider: `packaging/core/install.sh --hostname talaia.example.com --acme ovh --acme-email you@example.com`
   (the email is optional). The credentials go to `packaging/core/.env.acme` (mode 0600) and the core reads them as
   `OVH_ENDPOINT`, `OVH_APPLICATION_KEY`, `OVH_APPLICATION_SECRET` and `OVH_CONSUMER_KEY`.
3. Until the certificate is ready the panel answers with the certificate of the internal CA; then it swaps to the
   trusted one without a restart. The core logs `ACME certificate ready`. The account and the certificate are kept
   in the `core-data` volume (`/var/lib/talaia/acme`) and renewed 30 days before they expire.

The **agents port always uses the internal CA**, never the public certificate: agents pin the CA fingerprint, so
nobody who gets control of your DNS can impersonate the core and send commands to your servers.

> Written from the code and its tests with a simulated ACME server; not tried yet against Let's Encrypt or OVH.

### Alternative: a private VPN

If you prefer not to expose the panel, keep the core on a private network and reach it through a VPN such as
Tailscale: install it on the core machine and on your devices (follow its own documentation), add the VPN address
or name to `TALAIA_PUBLIC_HOSTNAMES` in `packaging/core/.env` separated by a comma, run `sudo docker compose up -d`
in `packaging/core` and open `https://<vpn-address>:<port>`. The core signs its certificate for every listed name.
This was written from the code and has not been tested on a real VPN.

## 7. Troubleshooting

| Symptom | Cause and fix |
|---|---|
| The browser says "not secure" | Expected with the internal CA. Accept the exception or trust `ca.pem`. |
| The six-digit code is refused | Wait for the next code and try again. After 5 wrong attempts the session is closed, and 5 failures lock the user for 15 minutes. Check the clock of both devices is right. |
| The panel asks for a recovery code you do not have | `user reset-2fa` (above). |
| `install.sh`: "does not match --ca-fingerprint" | The address answers with another CA. Check `--url` and copy the fingerprint printed by `token create` again. |
| The agent does not enroll | The token expired (15 minutes) or was already used: create another. |
| `./install.sh: command not found` | Run it with `bash install.sh …`, or `chmod +x` it. |
