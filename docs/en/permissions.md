# Users, permissions and API keys

[Leer en castellano](../es/permisos.md) · [Back to README](../../README.md)

This guide explains who can do what in Talaia, how far an assignment reaches, and how API keys,
single sign-on, invitations and terminal recordings fit in. The design is in
[architecture.md](architecture.md); the security model is in [security.md](security.md).

## The model in plain language

There is **one organization**. Inside it:

- **Server groups** put servers together (for example `production` or `clients`).
- **Teams** put users together so the same role can be given to several people at once.
- A **role assignment** gives one fixed role to a user, a team or an API key on one scope.

The scopes form a tree. An assignment applies to its node and everything below it:

```text
Organization
└── Group
    └── Server
        └── Stack
            └── Container
```

A stack or container is identified by its server and its name. A container that belongs to a Compose
project also inherits assignments made on that stack; a standalone container inherits only from the
server and above.

## The five roles

These are the only roles. Custom roles are not part of this version.

| Role                                    | What it can do                                                                                                                                                                                                                            | What it cannot do                                                                                                                                        |
| --------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Administrator** (`admin`)             | Everything allowed by the scope: users, settings, servers, stacks, containers, host operations, audit and recordings                                                                                                                      | Nothing inside Talaia, but it is equivalent to root on managed servers (see below)                                                                       |
| **Server operator** (`server_operator`) | Containers (view, logs, start/stop/restart, terminal), Docker resources, stacks (view, deploy, edit, remove containers), and the whole server: services, journal, apt, reboot, host terminal, agent updates                               | Manage users, teams, groups, invitations or API keys; change settings or OIDC; remove or enroll servers; read the audit log or recordings                |
| **App operator** (`app_operator`)       | View containers, stacks and servers; at server scope read services, apt status and the journal; at organization scope read audit and recordings; read container logs; start/stop/restart containers; deploy stacks (up/down/restart/pull) | Edit or adopt a Compose file; open a container or host terminal; change host services, apt or reboot; remove or enroll servers; manage users or settings |
| **Viewer** (`viewer`)                   | Read servers, stacks, containers and logs; when the scope reaches the server, read services, apt status and the journal; at organization scope, read the audit log and recordings                                                         | Change containers or stacks, open terminals, edit compose files, run host actions, remove or enroll servers, manage users or settings                    |
| **Metrics only** (`metrics_only`)       | Server, stack and container status and metrics                                                                                                                                                                                            | Logs, services, journal, apt, terminals, audit, recordings, any change and any administration                                                            |

The role's permissions are filtered by the assignment scope. For example, an administrator assigned
only to a stack gets the stack-level part of the administrator role, not users or host access.

## The minimum-scope rule

A permission is granted only when **the assignment scope is at least as wide as the permission's
minimum scope**:

| Minimum scope | Permissions                                                                                                                                                                                  |
| ------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Container     | `container.view`, `container.logs`, `container.control`, `container.terminal`                                                                                                                |
| Stack         | `stack.view`, `stack.deploy`, `stack.edit`, `container.remove`                                                                                                                               |
| Server        | `server.view`, `resources.manage`, `host.services.view`, `host.services.control`, `host.journal`, `host.apt.view`, `host.apt.upgrade`, `host.reboot`, `host.terminal`, `server.agent_update` |
| Organization  | `server.remove`, `server.enroll`, `users.manage`, `settings.manage`, `audit.view`, `recordings.view`, `apikeys.manage`                                                                       |

A group has no permissions of its own; it passes assignments down to its servers. An organization
assignment reaches everything.

The rule prevents privilege escalation. A server operator assigned to one stack can open a container
terminal **inside that stack**, but cannot see the host services, reboot the server or edit another
stack: host permissions need a server binding, and organization permissions need an organization
binding.

This does not make a stack binding harmless: `stack.edit` can run arbitrary code as root through the
Compose file. Give the server operator role, or a `stack.edit` permission, only to people you would trust
with root on that server. The app operator role deliberately does not have it.

## Why an app operator cannot edit Compose or open a terminal

A Compose file runs as root on the server. Whoever can edit it can mount `/`, start a privileged
container, use the host network or PID namespace, or add capabilities. Editing Compose is therefore
equivalent to root on the host, not an application-level operation.

The same applies to terminals: a root shell, or a shell in a privileged container, can change the
host. The app operator role deliberately has neither `stack.edit` nor `container.terminal` or
`host.terminal`. It can deploy the files that already exist, but it cannot change them.

The stack editor also shows a permanent note about this. See [security.md](security.md).

## Give access to a single app

1. Sign in as an administrator and open **Users**.
2. Choose an existing user's **Access**, or create an **Invite user** with the same access.
3. Select the role **Application operator**.
4. Set the scope to **Stack**, choose the server and type the stack name (for example `shop`).
5. Save. The user can now see that stack, deploy it and read its logs, but not edit its Compose file,
   open a terminal or reach the rest of the server.

An invitation also needs a username; email is optional. The invited person receives the link from
you; Talaia does not send email.

## 404, 403 and the audit log

The core decides, not the panel. The panel only hides controls the user cannot use.

- **404 Not Found** means the user cannot see the object at all. The panel does not reveal that a
  server, stack or container exists.
- **403 Forbidden** means the user can see the object, but the role does not allow that operation.
- A route with no declared permission is closed by default and the guardian test fails.
- Every denial is written to the audit log as `authz_denied`, with the actor, permission, method,
  path, result and scope (and the API key id when one was used). A denied request never exposes the
  object or the reason to the caller.

## The last administrator

Talaia always keeps at least one active user with the organization administrator role, directly or
through a team. The core refuses to disable the last one, or to remove or demote the last
organization-admin binding (including through a team), with `409 last_admin`. There is no user deletion
endpoint in this version. Users also cannot disable their own account.

## Invitations

- An administrator creates an invitation with an initial role and scope.
- The link looks like `https://panel.example.com:8443/invite#<token>`. The token is shown once and
  stored hashed.
- It expires after **72 hours** and works **once**.
- The invitee chooses a password (12–256 characters), and must set up TOTP before receiving a full
  session. The second factor is mandatory for everyone.
- Talaia sends no email; share the link through a channel you trust.

## API keys

API keys are for scripts and CI. They use their own role assignments and are always capped by the
owner's current permissions.

- **Create:** **Settings > API keys**, choose a name, lifetime and access, and type a fresh TOTP code.
- **Format:** `tlk_<12-character key id>_<32-character secret>`.
- **Shown once:** only a hash of the secret is stored. Copy it when the dialog appears; it cannot be
  recovered.
- **Lifetime:** 90 days by default; 365 days maximum.
- **Intersection:** a key can do only what both its own assignments and its owner's current
  assignments allow. If the owner loses access, the key loses it too. Disabling the owner revokes
  their keys.
- **Cannot:** open container or host terminals, manage users, teams, groups, keys or settings,
  reboot a server, or authenticate against sign-in endpoints.
- **Rate:** 600 requests per minute per key, with an additional per-IP bound.
- **Revoke:** from the same section, with a confirmation. Revocation is immediate.
- **Last used:** the list shows when the key was last used.

Create a key in the panel, then use it in a header. The panel shows this example:

```bash
curl -H "Authorization: Bearer $TALAIA_API_KEY" \
  https://panel.example.com:8443/api/v1/servers
```

A key does not need a cookie, a CSRF token or an `Origin` header. Do not put it in a URL or in a
shell command that other users can read.

## Single sign-on (OIDC)

Talaia supports one generic OpenID Connect provider.

1. In **Settings > Single sign-on**, enter the issuer URL, client ID, client secret and button label.
2. Register the redirect URL shown in the panel at the provider. It is normally
   `https://panel.example.com:8443/api/v1/auth/oidc/callback`; use the exact value shown.
3. Enable and save. Saving checks discovery; a bad issuer or unreachable provider returns
   `422 oidc_discovery_failed` and the configuration is not enabled.
4. The login page shows **Sign in with <label>** when the configuration is enabled.

The client secret is write-only: the core stores it encrypted and never returns it. Leaving the field
blank keeps the stored secret.

OIDC replaces the **password only**:

- The provider's email must be verified and must match an existing active user. There is no automatic
  account creation.
- On the first successful login the provider's `sub` is bound to that user. From then on the `sub` is
  what matters; a different subject for the same email is rejected.
- The second factor (TOTP or passkey) is still required.
- Failures return to the login with a generic message and are audited as `oidc_login_denied`.

Use HTTPS for the issuer, except `http://localhost` or `http://127.0.0.1` in development.

## Terminal recordings

Users with `recordings.view` (organization scope) find **Audit > Recordings**. The table shows when,
who, server, container or host, duration, close reason and a privileged badge. Finished sessions have
a **Download .cast** link.

```bash
asciinema play session.cast
```

API keys can never read or download recordings. Downloads are audited. The panel has no built-in
player. Recordings can contain secrets printed on screen; see [security.md](security.md).

## talaiactl

`talaiactl` uses an API key and the same API. The current commands are:

```text
login
servers list
containers list <server>
containers start|stop|restart <server> <container>
stacks list [server]
stacks up|down|restart|pull <server> <stack>
jobs wait <id>
version
help
```

`<server>` accepts the server ID or its exact name. If more than one server has that name, the command
stops and asks for the ID.

`talaiactl login` asks for the URL when it is missing, reads the key without echo when stdin is a
terminal (otherwise from one line), verifies it with `GET /api/v1/me`, prints the user name and saves the
configuration.

Configuration is read from:

- Linux: `~/.config/talaia/config.toml` (the directory returned by `os.UserConfigDir()`).
- Windows: `%AppData%\talaia\config.toml`.
- The file contains `url` and `api_key` and is created with mode `0600`. On Unix, talaiactl refuses
  to read it if group or other users can read it.

```toml
url = "https://panel.example.com:8443"
api_key = "tlk_..."
```

Environment variables and flags override the file:

| Setting  | Environment                       | Flag                                                      |
| -------- | --------------------------------- | --------------------------------------------------------- |
| URL      | `TALAIA_URL`                      | `--url`                                                   |
| API key  | `TALAIA_API_KEY`                  | `--api-key` (discouraged: it appears in the process list) |
| Extra CA | `TALAIA_CA_FILE`                  | `--ca-file`                                               |
| Language | `TALAIA_LANG` / `LC_ALL` / `LANG` | `--lang ca\|es\|en`                                       |

Examples:

```bash
talaiactl login
talaiactl servers list
talaiactl containers list vps-web-01
talaiactl containers restart vps-web-01 web
talaiactl stacks list
talaiactl stacks up vps-web-01 shop
talaiactl containers restart vps-web-01 web --wait --timeout 5m
talaiactl jobs wait <job-id>
talaiactl --json servers list
```

Actions print the job id. With `--wait`, they follow the job; `jobs wait` polls every 2 seconds up to
`--timeout` (10 minutes by default), prints the output and exits non-zero if the job failed. The
default output is an aligned table; `--json` prints the raw API data as JSON.

TLS uses the system roots plus `--ca-file` or `TALAIA_CA_FILE`; there is no option to skip
verification. The client sends `User-Agent: talaiactl/<version>`. Exit codes are 0 for success, 1 for
an error and 2 for wrong usage.

## Related documentation

- [Architecture](architecture.md) — data model and enforcement points.
- [Security](security.md) — threat model, recordings and what is audited.
- [Access](access.md) — password, TOTP, trusted devices and passkeys.
