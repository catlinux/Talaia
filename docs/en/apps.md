# Apps and deployments

[Leer en castellano](../es/apps.md) · [Back to README](../../README.md)

An app in Talaia is a normal Docker Compose project on one server. It comes from a GitHub repository
with a Dockerfile, a template or an image that already exists. Talaia keeps the source settings and the
deployment history, and creates or updates the Compose stack for you. There is no hidden format: the
stack stays on disk and keeps working with `docker compose` if you uninstall Talaia.

This guide explains the app screens, Git, variables, deployments and the two publishing modes. For the
step-by-step Apache or nginx setup on a server that already runs a web server, see [apache.md](apache.md).
The screen map is in [panel.md](panel.md); the permissions are in [permissions.md](permissions.md).

## Contents

[What an app is](#what-an-app-is) · [Permissions](#permissions) · [Create an app](#create-an-app) ·
[Git repositories](#git-repositories) · [Templates](#templates) · [Existing images](#existing-images) ·
[Variables](#variables) · [Deployments, logs, retry and rollback](#deployments-logs-retry-and-rollback) ·
[Domains and publishing](#domains-and-publishing) · [What Talaia touches](#what-talaia-touches) ·
[Troubleshooting](#troubleshooting)

## What an app is

An app has a name, a source and a server. The name follows the same rules as a stack: lowercase letters,
numbers, hyphens and underscores, up to 63 characters, and it must be unique on that server. It is also
the Compose project name and the image prefix `talaia/<name>`.

Talaia keeps the app in `/opt/talaia/stacks/<name>/compose.yaml`. In external mode the first deployment
also assigns a host port between 20000 and 29999 on `127.0.0.1`, so the app is reachable at
`127.0.0.1:<host-port>` on the server and nowhere else until your web server publishes it. In managed
mode the app joins the `talaia-proxy` network instead and Caddy publishes it on ports 80 and 443.

The app row in **Apps** shows its source, endpoint, whether it deploys on push and when it changed. Open
it to see **Configuration**, **Deployments** and **Variables**.

## Permissions

| Permission | What it allows |
| --- | --- |
| `stack.view` | See the app, its configuration, its variable names and flags (secret values stay hidden) and its deployment list. |
| `stack.deploy` | **Deploy now**, **Retry**, **Roll back**, and read the deployment log. API keys can have this permission for CI. |
| `stack.edit` | Create, change and delete the app, its variables and its domains. This is equivalent to root on the server. API keys cannot have it. |
| `server.proxy` | See the proxy mode, the certificate states and the external setup block in **Domains**; use the **App publishing** card and install, apply or remove the managed proxy. API keys cannot have it. |

A person who can edit a Compose file or an app already controls what runs as root on that server. Give
`stack.edit` and `server.proxy` only to people you would trust with root. The full role rules are in
[permissions.md](permissions.md).

## Create an app

Open **Apps** and press **New app**. The wizard has five steps: **Source**, **Build**, **Target**,
**Domain** and **Review**. The summary on the right updates as you move.

1. **Source** — choose **Git repository**, **Template** or **Existing image**.
2. **Build** — choose the repository and Dockerfile, the template, or the image; set the Compose service
   and the container port. The defaults are service `app`, port `3000`, Dockerfile `Dockerfile` and build
   context `.`.
3. **Target** — choose a server where you can create apps, choose the app name and decide whether it
   deploys on every push. Only servers where you have `stack.edit` appear.
4. **Domain** — optionally add the hostname. The step shows whether the server uses an external proxy or
   the managed Caddy proxy, and it explains who publishes the app. Talaia never edits Apache or nginx.
5. **Review** — check the summary and press **Create app**.

The app name is fixed after creation; it is also the stack directory and the image name. Choose it
carefully.

When the app is created, the panel shows **App created**. The first deployment is queued for Git and
image apps. A template app is also deployed if you have `stack.deploy`; otherwise the panel says that
someone with deployment permission has to start it from the app page. For a Git app, copy the values on
this screen now: they are not shown again.

The **Hostname** field is optional. This version adds domains from the wizard; there is no separate
form to add one later in the panel. If you skip it, the app still deploys, but on an external server no
Apache block is available until a domain exists. A warning such as **The hostname does not resolve.** or
**The hostname does not point to this server.** does not stop the app from being created; fix the DNS
record before you request a certificate.

## Git repositories

Talaia builds Git apps on the server from a Dockerfile. Only `github.com` repositories are accepted, over
HTTPS, `ssh://git@github.com/...` or `git@github.com:...`. The build context and the Dockerfile path are
relative to the repository and cannot leave it.

The **Build** step asks for **Repository URL**, **Branch**, **Dockerfile path** and **Build context**.
The repository is fetched at the current tip of the branch; submodules and Git hooks are not used.

For a private repository, add the deploy key to GitHub. For a public repository the clone works without
a key, but the app still has one.

### Deploy key

When a Git app is created, Talaia generates an ed25519 key pair. The success screen shows the **Deploy
public key** once. In the GitHub repository open **Settings > Deploy keys > Add deploy key**, paste the
key and leave **Allow write access** off. Talaia only reads the repository.

The private key is sealed in the core and never returned by the API or written to a log. The agent uses a
fixed set of GitHub host keys and checks them strictly. If the repository is private and the key is not
added, the first deployment fails while fetching the source; add the key and press **Deploy now** again.

### Webhook

A Git app also gets a **Webhook URL** and a **Webhook secret**, both shown once on the success screen. In
GitHub open **Settings > Webhooks > Add webhook** and fill in:

- **Payload URL**: the URL shown by Talaia. It ends in `/api/v1/hooks/git/<app-id>`.
- **Content type**: `application/json`.
- **Secret**: the value shown by Talaia.
- **SSL verification**: enabled.
- **Which events would you like to trigger this webhook?**: **Just the push event**.
- **Active**: enabled.

GitHub sends a `ping` when the webhook is created; Talaia answers it without deploying anything. Only a
push to the configured branch deploys. A push to another branch, a deleted branch or a duplicate delivery
is ignored. The webhook only deploys while **Deploy on every push** is enabled.

The endpoint is public because GitHub has to reach it; the request is authenticated by the
`X-Hub-Signature-256` HMAC-SHA256 signature made with the app secret. Treat the secret as a password and
do not reuse it. If the panel's public URL changes, update the payload URL in GitHub.

### Polling

If GitHub cannot reach the panel, enable **Also poll Git every 2 minutes** in the **Target** step instead
of using a webhook. Polling checks the branch from the core and queues a deployment when the commit
changes. It uses the same deploy key for private repositories, and it also needs **Deploy on every push**
enabled. The first check only records the current commit; later changes are deployed within about two
minutes.

### Pushes

By default a Git app has **Deploy on every push** enabled. Each push to the configured branch queues a
deployment. If a deployment is already waiting for that app, the newer one marks the waiting one as
**Superseded**; the deployment that is already running is not interrupted.

Disable **Deploy on every push** if you want to deploy only from the app page or from CI with an API key.
Webhook and polling events are ignored in that mode.

## Templates

**Templates** is a gallery of ready-made apps. Search by name, image or description; each card shows the
image, the container port and the recommended RAM. Press **Use template** to open the new-app wizard with
the template already selected; the app name is filled from the template id and can be changed.

The catalogue has fifteen applications — Forgejo, FreshRSS, Ghost, Homepage, Immich, Jellyfin, Mealie,
Miniflux, Nextcloud, Paperless-ngx, Plausible, Umami, Uptime Kuma, Vaultwarden and WordPress — and three
building blocks: PostgreSQL, MariaDB and Redis. The images are pinned.

Talaia generates a Compose stack for the template with random passwords. The template password is stored
as a secret and is not shown in the panel. A template does not build anything: the first deployment pulls
the pinned image and starts the stack. The RAM shown is a recommendation, not a limit.

The bundled templates publish on `127.0.0.1` and are intended for external mode. A deployed template
regenerates its Compose file from the template, so changes you make by hand to that stack are replaced on
the next deployment. If you need to keep changes, use an image or Git app, or keep a separate stack.

## Existing images

Choose **Existing image** to run an image that is already published. Enter a pinned reference such as
`ghcr.io/owner/app:1.2.3`, the Compose service and the container port. Talaia does not build anything and
does not use a deploy key or webhook; it generates the stack and starts the image.

The server must be able to pull the image. This version does not store registry credentials for apps, so
a private image needs whatever Docker authentication is already configured on the server.

## Variables

Open an app and use **Variables**. **Add variable** asks for a **Key**, a **Value**, **Secret** and
**Available at build time**.

- Keys use uppercase letters, numbers and underscores, start with a letter or underscore, and are at most
  128 characters.
- **Secret** values are write-only. The panel masks them and never returns the value; to replace one,
  enter a new value. Deleting a variable removes it from the app.
- **Available at build time** passes a non-secret variable to `docker build` as a build argument. Secret
  variables are never passed as build arguments, even if the flag is set.
- Variables are sealed in the core. They are not added to the container environment automatically by the
  Compose stack that Talaia generates for Git and image apps. If the app needs one at runtime, add it to
  the `environment:` section of the stack.

Changing a variable does not change a running container by itself. Deploy the app again for the new
build-time value to take effect.

## Deployments, logs, retry and rollback

A deployment is created when the app is created, when a push or the poller finds a new commit, with
**Deploy now**, with **Retry** or with **Roll back**. There is one deployment at a time per app; a newer
one replaces a deployment that is still waiting.

The pipeline fetches the source, builds the image when the source is Git, writes the stack, releases it
and, for Git builds, cleans old images. The app page lists the deployments with their state: **Queued**,
**Building**, **Releasing**, **Succeeded**, **Failed** or **Superseded**.

### Builds

- Git apps build on the app server with `docker build --pull` and the tag
  `talaia/<app>:<first-12-characters-of-the-commit>`.
- One build runs at a time on a server.
- The build is refused when the workspace filesystem has less than 2 GiB free. The source workspace is
  limited to 1 GiB and is removed after the build.
- A build has a 30 minute limit.
- Only non-secret variables marked **Available at build time** become build arguments, up to 128 of them.
- Template and image apps do not build.

### Logs and retry

Open **Logs** on a deployment to see the output in a drawer. While the deployment is **Queued**,
**Building** or **Releasing**, the drawer shows **Live** and refreshes. The log keeps the last 256 KiB;
older output is dropped from the beginning. Logs need `stack.deploy`.

**Retry** appears on a failed deployment with a commit. It queues the same commit again. A failed build
does not touch the running stack. For a Git release, a failure restores the previous Compose file.

### Zero downtime

**Zero downtime** is available only when all of these are true:

- the app has **Zero downtime** enabled;
- the server uses the managed proxy;
- the stack has a single service;
- that service has no volumes, no published host ports and no fixed `container_name`;
- the service does not ask for more than one replica.

Talaia starts a second container, waits up to 120 seconds for the Docker health check, or for a TCP
connection to the app port when there is no health check, removes the old container and scales back to
one. If the new container does not become healthy, it is removed and the old one keeps serving; the
deployment is marked **Failed**.

External mode publishes the app on `127.0.0.1`, so the second container cannot bind the same host port.
Every deployment behind your own Apache or nginx therefore has a cut of a few seconds. The app page says
so under **Zero downtime**.

### Images and rollback

Successful Git builds are kept as the last three images of `talaia/<app>`. Older ones may be removed after
a release. **Roll back** on a successful deployment deploys that commit's image again without building;
it needs the image to still exist. Talaia removes only images whose name starts with `talaia/<app>:`. It
never runs a global Docker prune and never removes another app's images.

After the first deployment, a Git or image app whose Compose file was edited by hand keeps the rest of
the file; each deployment changes only the `image:` of the configured service. A template app is
regenerated from its template instead.

## Domains and publishing

**Domains** lists the hostnames of the apps you can see. The **State** column shows:

- **Handled by your Apache** for an external server: Talaia does not manage the certificate.
- **Managed by Caddy** for a managed server when no certificate state is available.
- **Certificate valid**, **Certificate pending** or **Certificate failed** for a managed domain with
  certificate details.
- **Unknown** when the state cannot be determined.

A warning says **The hostname does not resolve.** or **The hostname does not point to this server.** when
the DNS check fails. The check does not block the app.

The proxy state and the external setup block need `server.proxy`. With only `stack.view` the domain can
still be listed, but it shows **Unknown** and has no **View setup** button.

### External proxy

External mode is the default. The app listens on `127.0.0.1:<host-port>` and your own Apache or nginx
publishes it. Talaia never edits those files. The first deployment assigns the host port between 20000 and
29999; until then the setup block is not ready.

In **Domains**, open **View setup** on a row with **Handled by your Apache**. The drawer shows the
hostname and `127.0.0.1:<host-port>`, the **Apache** block, the **nginx** block and the **Steps**. The same
note applies: *Talaia never changes Apache or nginx. Paste the block into your web server configuration
yourself.* Use **Copy** to copy a block or the steps; the button changes to **Copied**.

The full walkthrough, including certificates and removal, is in [apache.md](apache.md).

### Managed proxy (Caddy)

On a server where ports 80 and 443 are free, the **App publishing** card on the server page can install a
managed Caddy proxy. Caddy 2.10.2 is pinned by digest and listens only on those ports; the apps join the
`talaia-proxy` network and Caddy gets a certificate for each hostname automatically.

Installing needs `server.proxy`. The card only offers **Install managed proxy** when the agent reports
both ports free; if one is in use or the agent cannot confirm it, it says so and external mode remains the
option. After a domain changes, use **Apply routes** to load the new Caddy configuration. **Remove managed
proxy** stops Caddy and removes its stack but keeps the apps and their data.

Choose the mode before creating apps. Installing or removing the proxy does not rewrite existing
hand-edited stacks, and the bundled templates are intended for external mode.

## What Talaia touches

- The app's Compose project in `/opt/talaia/stacks/<app>/`, including `compose.yaml` and an existing
  `.env` when it updates a hand-edited stack.
- Build workspaces under `/var/lib/talaia/builds/<app>/`, which are removed after each build.
- Images named `talaia/<app>:<commit>`. It keeps the last three successful ones and removes only other
  images with that exact prefix.
- In managed mode, the Caddy stack under `/var/lib/talaia/proxy/` and the `talaia-proxy` network.
- The container label `dev.talaia.app` and, in external mode, a host port between 20000 and 29999 on
  `127.0.0.1`.

It does **not** touch Apache, nginx, your other Compose projects, other containers or other images. The
stacks Talaia generates for Git and image apps do not mount the Docker socket, do not run privileged and
set a 1 CPU and 512 MB memory limit. Anyone with `stack.edit` can change that, which is why the permission
is equivalent to root. Talaia never runs a global Docker prune.

Deleting the app record through the API does not remove its Compose stack or its website; removal is
explained in [apache.md](apache.md#remove-an-app-and-its-site).

## Troubleshooting

**The first deployment failed.** Check that the repository is reachable, that a private repository has
the deploy key, that the Dockerfile path and build context exist, and that the server has at least 2 GiB
free. Open **Logs** on the failed deployment; the output names the failing step.

**A push does not deploy.** Check that **Deploy on every push** is enabled, that the webhook points to the
current URL, that **Content type** is `application/json`, that the secret matches and that the push is to
the configured branch. GitHub's recent deliveries show the response. If you use polling, check that
**Also poll Git every 2 minutes** is also enabled.

**The domain shows a warning.** Create the DNS record for the hostname and point it to the server. The app
can be created before DNS is ready, but certificate issuance waits for it.

**The site returns 502.** The app or its container is not answering on the host port. See
[apache.md](apache.md#troubleshooting).

**The certificate cannot be issued.** Check the DNS record, that ports 80 and 443 reach the server and
that the hostname in the Apache or nginx site is the same one you requested. The commands and logs are in
[apache.md](apache.md#troubleshooting).

**A rollback fails.** The image of that deployment may already have been removed. Talaia keeps the last
three successful images; deploy the commit again if you need an older one.

**An image cannot be pulled.** This version does not store registry credentials. Configure Docker
authentication on the server, or use a public image.
