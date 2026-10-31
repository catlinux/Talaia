# Publishing an app behind Apache or nginx

[Leer en castellano](../es/apache.md) · [Back to README](../../README.md)

This guide is for **external mode**: a server that already runs Apache or nginx with other sites, where
Talaia publishes each app on `127.0.0.1` and you add the site by hand. Talaia never changes Apache, nginx
or their certificates. If the server can give ports 80 and 443 to Talaia, the managed Caddy mode is
explained in [apps.md](apps.md#managed-proxy-caddy); then you do not need this guide.

The app itself is explained in [apps.md](apps.md). The screen map is in [panel.md](panel.md).

## Contents

[Before you start](#before-you-start) · [1. Point the domain to the server](#1-point-the-domain-to-the-server) ·
[2. Create the app and its domain](#2-create-the-app-and-its-domain) ·
[3. Wait for the first deployment](#3-wait-for-the-first-deployment) ·
[4. Copy the setup block](#4-copy-the-setup-block) ·
[5. Save and enable the Apache site](#5-save-and-enable-the-apache-site) ·
[6. Get the certificate](#6-get-the-certificate) · [7. Verify](#7-verify) ·
[8. Deploy on every push](#8-deploy-on-every-push) · [nginx variant](#nginx-variant) ·
[Remove an app and its site](#remove-an-app-and-its-site) · [Troubleshooting](#troubleshooting)

## Before you start

- A DNS record for the app hostname, pointing to the server.
- Apache installed and running, with ports 80 and 443 reachable from the internet.
- `certbot` and its Apache plugin, or permission to install them.
- `stack.edit` on the app server to create the app; `server.proxy` on that server to see the proxy mode
  and copy the setup block in **Domains**. `stack.view` lets you see the app and its deployments, but not
  the block.
- The server in external mode. Talaia never installs Apache or nginx and never edits their files.

Use example values in this guide: hostname `app.example.com`, server address `192.0.2.10`.

## 1. Point the domain to the server

Create an **A** record for `app.example.com` with the server's IPv4 address. If the server has IPv6, add
an **AAAA** record too. With the common tools:

```
dig +short app.example.com
```

The answer must contain the server address, for example `192.0.2.10`. DNS changes can take a few minutes.
Talaia checks this when you add the domain and shows **The hostname does not resolve.** or **The hostname
does not point to this server.** if it cannot confirm it. You can create the app before DNS is ready, but
certificate issuance waits for it.

## 2. Create the app and its domain

Open **Apps** and press **New app**. Choose the source, build and target as described in
[apps.md](apps.md#create-an-app). In the **Domain** step:

1. Enter `app.example.com` in **Hostname**.
2. Check that the step says **External proxy**. The app then listens on `127.0.0.1` and your web server
   publishes it.
3. Continue to **Review** and press **Create app**.

The first deployment starts automatically for a Git or image app. A template app starts automatically if
you have `stack.deploy`; otherwise open the app and press **Deploy now** with an account that has that
permission.

If you already created the app without a domain, there is no separate form in the panel in this version.
Add it through the app API (`POST /api/v1/apps/<app-id>/domains`) with a signed-in session and a JSON body
such as `{"hostname":"app.example.com"}`, then continue with the first deployment. The hostname must be
valid and unique.

## 3. Wait for the first deployment

The setup block needs a host port, and the host port is assigned on the first deployment. Open the app
and check:

- **Deployments** shows a **Succeeded** deployment.
- **Configuration** shows a **Host port**, for example `20431`, and an **Endpoint** such as
  `127.0.0.1:20431  app:3000`.

If the deployment failed, open **Logs** and fix the cause before continuing. Common causes are a missing
deploy key, a wrong Dockerfile path or build context, a container port that the app does not listen on,
or less than 2 GiB free on the server.

In external mode the host port is chosen between 20000 and 29999. It belongs to this app; do not reuse it
in another stack.

## 4. Copy the setup block

Open **Domains** and find `app.example.com`. An external domain shows **Handled by your Apache**, even
when the web server is nginx. Press **View setup**.

The drawer shows:

- the hostname and `127.0.0.1:<host-port>`;
- the **Apache** block;
- the **nginx** block;
- the **Steps** with the commands for both web servers.

Read the note: *Talaia never changes Apache or nginx. Paste the block into your web server configuration
yourself.* Use **Copy** on the Apache block, then **Copy** on the steps if you want them in a terminal.
The button changes to **Copied**.

The block is for port 80 only. `certbot --apache` writes the HTTPS virtual host and the certificate after
the HTTP site works. Do not add `SSLCertificateFile` by hand before the certificate exists.

## 5. Save and enable the Apache site

Create the site file with the Apache block from the drawer:

```
sudo nano /etc/apache2/sites-available/app.example.com.conf
```

Paste the block, save and exit. Then enable the modules it needs and the site. The module command is safe
to repeat if a module is already enabled.

```
sudo a2enmod proxy proxy_http proxy_wstunnel headers rewrite
sudo a2ensite app.example.com
sudo apachectl configtest && sudo systemctl reload apache2
```

`a2ensite` takes the site name without `.conf`. `configtest` must finish with `Syntax OK`; if it reports
an error, fix the file before reloading. A reload keeps your other sites running.

The block uses `ProxyPreserveHost`, forwards the original protocol, and upgrades WebSocket connections.
Leave the other virtual hosts alone: Talaia did not create them and does not change them.

## 6. Get the certificate

Check again that the hostname resolves to this server and that port 80 answers. Then install Certbot if
it is not already there:

```
sudo apt update
sudo apt install certbot python3-certbot-apache
```

Request the certificate:

```
sudo certbot --apache -d app.example.com
```

Certbot asks for an email address and agreement the first time. Choose the redirect to HTTPS when it
offers one. It writes the HTTPS virtual host and configures renewal. Do not copy the port-80 block into
`default-ssl` or add the certificate by hand.

Check the result and the renewal timer:

```
sudo certbot certificates
systemctl list-timers | grep certbot
```

## 7. Verify

```
curl -I http://app.example.com
curl -I https://app.example.com
```

The HTTP request should redirect to HTTPS, and the HTTPS request should return the app's status, normally
`200`. Open `https://app.example.com` in a browser and check that the app works, including any interactive
or WebSocket feature you use.

On the server, `sudo apachectl -S` lists the virtual hosts and the site file that serves the name. It is
useful when another site answers first.

## 8. Deploy on every push

With **Deploy on every push** enabled and the GitHub webhook or polling configured, a push to the
configured branch queues a new deployment. The app page shows it under **Deployments**; the Apache and
nginx configuration does not change. Each external deployment has a cut of a few seconds because the app
publishes a fixed host port. If the deployment fails, the old container keeps serving.

If you deploy manually, press **Deploy now** and wait for **Succeeded** before checking the site.

## nginx variant

The same drawer gives the nginx block. Use it when nginx owns ports 80 and 443:

```
sudo nano /etc/nginx/sites-available/app.example.com
```

Paste the **nginx** block, save it and enable the site:

```
sudo ln -s /etc/nginx/sites-available/app.example.com /etc/nginx/sites-enabled/
sudo nginx -t && sudo systemctl reload nginx
```

Then request the certificate with the nginx plugin:

```
sudo certbot --nginx -d app.example.com
```

`nginx -t` must pass before the reload. Certbot writes the HTTPS server and the redirect. The rest of
your nginx sites are not touched.

## Remove an app and its site

This version has no **Delete app** button in the panel. Remove the app through the API, then remove its
website. The app id is the last part of the app page address, `/apps/<id>`. While signed in to the panel,
run this in the browser console on the panel tab, with the app name where `<app-name>` is:

```js
fetch('/api/v1/apps/<app-id>', {
  method: 'DELETE',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ confirm_name: '<app-name>' }),
})
```

It needs `stack.edit` on the app and uses your current session; an API key cannot delete an app. The
confirmation must be the app name, the Compose stack name. The request is refused with `409` when a
deployment is building or releasing, or when the server agent is offline. It returns `200` with an
`output` that summarises what was removed. Talaia stops and removes the containers and the Compose files,
removes the images `talaia/<app>:*` and the build workspace, and keeps the named volumes, so the app data
is not destroyed. In managed mode it also removes the app from the Caddy routes.

Remove the Apache site:

```
sudo a2dissite app.example.com
sudo rm /etc/apache2/sites-available/app.example.com.conf
sudo apachectl configtest && sudo systemctl reload apache2
```

If no other site uses the certificate, remove it too:

```
sudo certbot delete --cert-name app.example.com
```

Keep it if another virtual host still uses the same name. For nginx, remove the symlink and the file:

```
sudo rm /etc/nginx/sites-enabled/app.example.com
sudo rm /etc/nginx/sites-available/app.example.com
sudo nginx -t && sudo systemctl reload nginx
```

The API already removed the stack and images; you do not need to remove them from the server page. Named
volumes stay on the server, so remove them separately if you no longer need the data. The DNS record can
stay if you plan to reuse the name; otherwise remove it at your DNS provider.

## Troubleshooting

### The site returns 502 Bad Gateway

The web server reached the host port but the app did not answer. Check, in this order:

1. The latest deployment is **Succeeded**; a failed deployment can leave the old container serving.
2. The port in the Apache or nginx block matches **Host port** on the app page. If the app was recreated
   or the block is old, copy it again from **Domains > View setup**.
3. On the server, the app is listening on the host port:

   ```
   sudo ss -ltnp | grep <host-port>
   curl -I http://127.0.0.1:<host-port>
   ```

4. The container is running and its logs do not show a crash:

   ```
   cd /opt/talaia/stacks/<app>
   sudo docker compose ps
   sudo docker compose logs --tail=100
   ```

5. The app listens on all interfaces inside the container, not only on `127.0.0.1` inside it. Docker
   forwards to the container address, so a process bound only to the container loopback is not reachable
   through the published port.

### The certificate is not issued

- `dig +short app.example.com` must return the server address. Remove a wrong AAAA record if IPv6 does
  not reach the server.
- Port 80 must answer from the internet. Check the firewall and your hosting provider's security group.
- The `ServerName` in the Apache block, or `server_name` in nginx, must be exactly the hostname passed to
  Certbot.
- Read `/var/log/letsencrypt/letsencrypt.log` and run `sudo certbot certificates`.
- If you hit a rate limit, wait and try again. Do not delete the working HTTP site while debugging.
- After a successful renewal, `sudo certbot renew --dry-run` checks the renewal path without changing the
  certificate.

### Another site answers, or the app shows a 404

Run `sudo apachectl -S` and check which site file serves `app.example.com`. The site must be enabled,
loaded and use the same name. In nginx, `sudo nginx -T` shows the loaded configuration. A missing
`a2ensite` or a reload that never ran leaves the new site inactive.

### The certificate is for the wrong domain

The browser and the certificate must use the same name. Check the DNS record, the `ServerName` or
`server_name`, and the hostname in **Domains**. A certificate for a different name is not fixed by
copying the block; request the certificate for the correct name with `certbot --apache -d <hostname>` or
`certbot --nginx -d <hostname>`.

### WebSockets or live updates do not work

Check that the modules are enabled (`sudo a2enmod proxy proxy_http proxy_wstunnel headers rewrite`) and
that the block in the site is the one from the drawer. The Apache block upgrades WebSocket requests; nginx
forwards the `Upgrade` and `Connection` headers. A proxy or CDN in front of the server must also allow the
upgrade.

### There is no setup block

The app has no host port yet, which normally means the first deployment has not succeeded. Open the app
and check **Host port** and **Deployments**. If the server is in managed mode, external blocks do not
apply; use [apps.md](apps.md#managed-proxy-caddy) instead.
