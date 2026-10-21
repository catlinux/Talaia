# Installation

[Leer en castellano](../es/instalacion.md) · [Back to README](../../README.md)

Talaia has two parts: the **core** (panel, API and database), which runs in Docker on one machine, and an
**agent** on every server you want to manage.

> **Status.** The manual path (section 2, "Without the script") was run end to end on a real Debian 13
> machine. The one-command core installer and the agent download from the core were checked piece by piece
> (script syntax, CA fingerprint check and package download against a real TLS server, Go tests), but not yet
> end to end on a clean machine. If a step here does not match what you see, please open an issue.

## 1. Requirements

- **Core machine:** Debian 13 (or Ubuntu), `git`, `curl`, and Docker Engine with the Docker Compose plugin. If
  Docker is missing, the installer explains how to add it and can run the official script with `--install-docker`
  (or when you answer yes).
- **Servers to manage:** Debian or Ubuntu with systemd, `curl`, and root access (`sudo`).
- A name or IP address the servers and your browser can reach the core at, and two free TCP ports for it
  (8443 by default); a second port is used by the agents (8444 by default).
- An authenticator app that reads TOTP codes (Google Authenticator, Aegis, 2FAS, Bitwarden…).

## 2. Install the core

On a fresh Debian or Ubuntu the one-line bootstrap clones the current `main` into `/opt/talaia` and runs the
installer. Before 1.0 this installs `main`, not a tagged release:

```bash
curl -fsSL https://raw.githubusercontent.com/catlinux/Talaia/main/install-core.sh | bash
```

Pass installer flags after `bash -s --`; for a public name with a trusted certificate at OVH:

```bash
curl -fsSL https://raw.githubusercontent.com/catlinux/Talaia/main/install-core.sh | bash -s -- \
  --hostname talaia.example.com --acme ovh
```

The bootstrap uses `TALAIA_DIR` (default `/opt/talaia`), `TALAIA_REPO_URL` (default
`https://github.com/catlinux/Talaia.git`) and `TALAIA_BRANCH` (default `main`). It creates the directory with
`sudo` only when its parent is not writable, leaves the clone owned by your user (the update helper runs git as
that owner) and reuses an existing clone of the same repository without pulling; if the directory holds
anything else it stops. It needs bash and git and, on Debian or Ubuntu, offers to install git with apt-get.

The second option is to clone and run the installer yourself:

```bash
git clone https://github.com/catlinux/Talaia.git
cd Talaia
packaging/core/install.sh --hostname 192.0.2.10 --username admin
```

Replace `192.0.2.10` with the address the panel will be reached at (in a terminal it asks for it when you omit
`--hostname`). The script:

1. checks the machine before writing anything: Debian or Ubuntu (anything else only warns), `git`, a way to
   get root (root or `sudo`; if `sudo` needs a password it says so), at least 2 GiB free on the filesystem
   that holds `/var/lib/docker` (it warns below 5 GiB) and Docker with the Compose plugin. Every missing piece
   comes with the command that fixes it. If Docker is missing, `--install-docker` runs
   `curl -fsSL https://get.docker.com | sudo sh`; in a terminal the script offers it first, and it never
   installs it otherwise. If the Compose plugin is missing, it names
   `sudo apt-get install -y docker-compose-plugin` (or `docker-compose-v2` on some systems);
2. chooses the ports: the first free one from 8443 for the panel and from 8444 for the agents (checked with
   `ss`), never 80 or 443 on its own. `--port` and `--agent-port` force a port; if it is busy the script stops;
3. when `--hostname` is a DNS name, checks that it resolves. It warns when it does not, or when it points to an
   address that is not one of this machine (normal behind provider NAT), and continues;
4. writes `packaging/core/.env` with a random database password and those ports (an existing `.env` is never
   replaced, ports included);
5. prints the exact firewall commands for the two ports when ufw or firewalld is active, and always reminds
   you about the provider's firewall or security group. It never opens a port;
6. installs the update helper `talaia-hostd` with `sudo` (see section 5), reports whether
   `talaia-update.path` is active, and then builds the image and starts the stack with
   `docker compose up -d --build`. The first build takes a few minutes;
7. waits for the core to answer and checks the panel (`/healthz`) and the agent port (`/ca.pem`) from this
   machine, printing `ok` or `FAILED` for each;
8. creates the administrator, asking for the password twice (at least 12 characters).

With `--acme ovh` (or answering yes when it asks, in a terminal) it also prepares a trusted certificate for the
panel by ACME DNS-01. It asks for the DNS zone (default: the last two labels of the host name; for names like
`.co.uk` type the registered domain, or pass `--acme-zone`) and the OVH region (`ovh-eu` or `ovh-ca`; also read
from `OVH_ENDPOINT`), prints the prepared key-creation link and the guide [docs/en/ovh-dns.md](ovh-dns.md), and
asks for the application key, secret and consumer key (the last two without echo), or reads them from
`OVH_APPLICATION_KEY`, `OVH_APPLICATION_SECRET` and `OVH_CONSUMER_KEY`. It checks them with the OVH API before it
writes `packaging/core/.env.acme` (mode 0600, ignored by git) or builds anything: a missing right is named, a key
that is not validated or is rejected is explained, and it offers to re-enter the keys, continue with the Talaia
CA (ACME off) or abort. `--no-acme-check` skips the network check for an offline install. An existing
`.env.acme` is checked too. `--acme-email` sets the contact address. After the stack starts it waits up to three
minutes for `ACME certificate ready` and says whether the certificate was obtained or is still pending. See
section 6.

The final lines repeat the panel and agent URLs, how to add a server, how to update from **Settings**, where the
logs are and whether the update helper is active. It is safe to run again. If your user cannot talk to Docker, it
uses `sudo docker` on its own.

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

- **Update the core from the panel:** the **Settings** screen shows the running version and the newest commit of
  `main` whose CI is green, with the commits you would get. **Update** asks for confirmation and hands the work to
  the host helper (below); the panel is unavailable for a few minutes and follows the progress on its own.
- **Update the core by hand:** `git pull`, then in `packaging/core` run
  `sudo TALAIA_GIT_COMMIT=$(git rev-parse HEAD) docker compose up -d --build` (without the variable the panel shows
  the version as unknown).
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

### How updates from the panel work

The core runs in a read-only container without the Docker socket, so it cannot update itself. `install.sh` installs
a small root helper on the host, **`talaia-hostd`** (skip it with `--no-hostd`):

- The core only writes a request, `{"target":"<commit>"}`, in `/var/lib/talaia-host/requests/` (the one directory
  it may write there) and reads `/var/lib/talaia-host/status.json`.
- `talaia-update.path` sees the request and starts `talaia-update.service`, which runs
  `/usr/local/sbin/talaia-hostd update`. It moves the request out of the core's reach and accepts only a 40-character
  commit that is on `origin/main` and newer than the running one; any other branch, path or field is refused.
- It backs up the database to `/var/lib/talaia-hostd/backups` (the last three, readable only by root), fast-forwards
  the clone (git runs as the owner of the clone, not as root), rebuilds and waits up to three minutes for `/healthz`.
- If the new core does not answer, it goes back to the previous commit, restores the backup in one transaction and
  rebuilds that version. The panel shows the result and the last log lines; the full log is in
  `sudo journalctl -u talaia-update`. What the panel recorded between the backup and the rollback (audit entries,
  metrics) is lost.
- When an update changes the helper itself, it installs the new version once the new core answers, so the next
  update already uses it.
- A request the helper does not take within five minutes makes the panel say that the helper is not running
  (check `systemctl status talaia-update.path`); a request older than 30 minutes is refused instead of applied
  late.

Its settings are in `/etc/talaia/hostd.conf` (`REPO_DIR`, `REPO_USER`). The clone must stay on `main` without local
changes. Whoever owns the clone can change what gets built as root, so keep it under an administrator account.

## 6. Running the core on a public server

The agents connect **outbound** to the core, so the best place for the core is a machine with a public IP address
(a VPS): servers behind a home router or any NAT enroll and stay connected without a VPN or an open port on their
side.

- **It does not need ports 80 or 443.** The installer picks free ports (8443 and 8444 by default), so a VPS that
  already runs a web server or a reverse proxy keeps working. Open the two chosen ports in the firewall of the VPS
  (they are written to `packaging/core/.env`): when ufw or firewalld is active the installer prints the exact
  commands (`sudo ufw allow <port>/tcp`, or
  `sudo firewall-cmd --permanent --add-port=<port>/tcp && sudo firewall-cmd --reload`) and never opens a port
  itself. Also open them in the firewall or security group of your provider.
- **DNS:** point an `A` (or `AAAA`) record at the VPS. When `--hostname` is a DNS name the installer checks it,
  warns if it does not resolve or if it resolves to an address that is not one of the machine (normal behind
  provider NAT), and continues either way.
- **Install with the public name:** `packaging/core/install.sh --hostname talaia.example.com --username admin`.
- **The panel is then on the internet.** What protects it is the password plus the second factor, the rate limit
  and the lockout after 5 failed sign-ins (see section 7). Keep the password long and the authenticator safe.

### A certificate browsers trust (ACME DNS-01)

With the internal CA the browser warns that the panel is "not secure". The core can get a certificate from Let's
Encrypt on its own through DNS-01, which proves the name with a DNS record: it works on any port and needs no web
server, so Apache or anything else already on 80/443 is not touched. It needs a DNS name in `--hostname` (an IP
address cannot get one). In a terminal the installer asks which way to use:

1. **Two DNS records (recommended, any DNS provider):** you create an `A` record and an `NS` record that delegates
   `_acme-challenge.<name>` to the server, and the `acme-dns` service answers the challenge on port 53. No API key.
   The installer prints both records with your values, asks before opening the ports, checks the records, proves
   the setup with Let's Encrypt staging and then gets the real certificate. Step by step, with OVH:
   [dns-delegation.md](dns-delegation.md) (also in [castellano](../es/dns-delegation.md) and
   [català](../ca/dns-delegation.md)). Flags: `--acme delegation`, and `--public-ip <IPv4>` behind NAT.
2. **An OVH API key:** allowed to change only your zone; the installer prints a prepared link and checks the key
   before building. Guide: [ovh-dns.md](ovh-dns.md). Flag: `--acme ovh`. The key goes to `packaging/core/.env.acme`
   (mode 0600).
3. **None for now** (`--no-acme`): the internal CA; run the installer again with `--acme delegation` later.

Until the certificate is ready the panel answers with the certificate of the internal CA; then it swaps to the
trusted one without a restart. The account and the certificate are kept in the `core-data` volume
(`/var/lib/talaia/acme/<directory>`) and renewed 30 days before they expire. Opened by IP address, the panel keeps
answering with the certificate of the internal CA.

The **agents port always uses the internal CA**, never the public certificate: agents pin the CA fingerprint, so
nobody who gets control of your DNS can impersonate the core and send commands to your servers.

> Written from the code and its tests (a real DNS check against the zone, a simulated ACME server); not tried yet
> end to end against Let's Encrypt.

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
