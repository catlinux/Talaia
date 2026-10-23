# Security

[Leer en castellano](../es/seguridad.md) · [Back to README](../../README.md)

What the panel can do to your servers, what protects it, and what is recorded. The design behind it is in
[architecture.md](architecture.md); the installation steps are in [installation.md](installation.md).
The user guide for the server screen is [server.md](server.md).

## Administrators and server operators are root on the servers they manage

The **administrator** role is protected by a password and a second factor (TOTP). **That account is
equivalent to root on every server it manages.** The other roles reduce the reach, but any role that can
edit Compose, run host commands or open a privileged terminal is still root-equivalent inside its scope.
The rules are in [permissions.md](permissions.md). This is by design, not an oversight, because several
things the panel offers already give root:

- **A Compose file runs as root on the server.** Whoever can edit one can mount `/`, start a `privileged`
  container, use `pid: host` or `network_mode: host`, add capabilities or build an image. Talaia does not filter
  the contents: a list of dangerous fields would only give a false sense of safety, because there are too many
  ways. The stack editor says so in a note that cannot be dismissed.
- **Adopting an existing project** runs its Compose file, from where it already is, as root. The owner of that
  directory can change what root runs on the next start, exactly as before installing Talaia.
- **The container terminal** opens a shell inside a container. If the container is `privileged`, shares the host
  PID namespace or mounts the Docker socket, that shell is root on the server; the panel warns before opening it
  and records `privileged` in the audit log.
- **The host terminal** opens a root login shell on the server itself. It needs a confirmation and a fresh
  TOTP code, is recorded like a container terminal and is audited. See [server.md](server.md).
- **Updates from the panel** are applied by a root helper on the core machine ([installation.md](installation.md),
  section 5). The core can only ask for a commit of `origin/main` that is newer than the running one.

Keep administrator and server-operator accounts safe: long, unique passwords, an authenticator you control,
and no sharing. Per-user roles and scopes now exist, but the permissions to edit stacks, adopt projects and open
terminals remain root-equivalent and are never granted to a viewer or an app operator.

## Sign-in methods

The base is the administrator password plus TOTP, with the recovery codes as the way back in. Two optional
methods make regular sign-in faster:

- **A PIN on a trusted device.** From **Settings > Access**, a signed-in browser becomes a trusted device with a
  6-12 digit PIN and the current password. The core sends it a 180-day `__Host-talaia_device` cookie and stores
  only its hash; the PIN is useless without that cookie and is stored hashed. Five wrong PINs disable the
  device. TOTP is asked again when the browser is closed or after `device_mfa_hours` (default 12, range 1-168),
  and a user can have at most 10 trusted devices. Changing the password, or resetting the TOTP secret, removes
  every trusted device.
- **Passkeys.** WebAuthn credentials with user verification (fingerprint, face or the device PIN). A passkey
  counts as the password plus the second factor, so it opens a full session without TOTP. A user can have at
  most 10 passkeys. They need HTTPS with a DNS name in `public_hostnames`; an IP address cannot be the relying
  party, and with Talaia's own CA some browsers may refuse them. Challenges expire after five minutes, and a
  signature counter that goes backwards disables the passkey.

- **Single sign-on (OIDC).** A configured OpenID Connect provider can replace the password for an existing
  user whose verified email matches. The second factor is still required. Provider failures return to a
  generic login error and are audited; see [permissions.md](permissions.md).
These methods are additive: the password, TOTP and recovery codes remain the base and the recovery path. Setup,
use, removal and troubleshooting: [access.md](access.md).

## Users, invitations and denials

- Five fixed roles can be assigned at organization, group, server, stack or container scope. A permission
  only counts when the scope is at least its minimum level; a stack-bound server operator never reaches the
  host. See [permissions.md](permissions.md).
- A user who cannot see an object gets **404**, not a hint that it exists. A user who can see it but lacks
  the operation gets **403**. The panel only hides controls; the core makes every decision.
- Every denial is audited as `authz_denied` with the actor, permission, method, path, result and scope; API
  key denials also include the key id. The response never contains the reason.
- An invitation link is single-use, expires after 72 hours, and the token is stored hashed. Anyone who gets
  the link before it is used can claim the account, so send it through a trusted channel. Talaia sends no
  email. The invitee sets a password and must set up TOTP before a full session.
- At least one active organization administrator must remain. Disabling the last one, or removing or
  demoting the last organization-admin assignment (direct or through a team), is refused with
  `409 last_admin`; nobody can disable their own account.

## API keys

- API keys are scoped credentials for scripts. They are shown once, stored as a SHA-256 hash, and expire
  after 90 days by default (365 maximum).
- Effective permissions are the intersection of the key's own assignments and its owner's current
  assignments. If the owner loses access, the key loses it too.
- A key can never open a container or host terminal, manage users, teams, groups, keys or settings, reboot a
  server, or use the sign-in endpoints. Routes marked `noAPIKey` reject keys even when their role bindings
  would allow them.
- Keys use `Authorization: Bearer tlk_…`, need no cookie or CSRF token, and are limited to 600 requests per
  minute per key, with an additional per-IP bound. Creating one needs a fresh TOTP code.
- Treat the key as a password: do not put it in URLs, logs or shell history, and prefer `TALAIA_API_KEY` or
  `talaiactl login` over `--api-key`, which appears in the process list. Revoke it immediately if leaked.

## Single sign-on (OIDC)

- The provider replaces the password only. TOTP or a passkey is still required, so a compromised identity
  provider alone does not open the panel.
- The email claim must be verified and match an existing active user. There is no automatic account
  creation. On the first login the provider's `sub` is bound to that user; a different subject for the same
  email is rejected.
- The flow uses discovery, authorization code with PKCE, `state` and `nonce` in a sealed 10-minute cookie,
  and verifies the ID token. The client secret is write-only and stored encrypted.
- Only `https` issuers are accepted, except `http://localhost` or `http://127.0.0.1` in development.
- Failures redirect to a generic login error and are audited as `oidc_login_denied`; they do not reveal
  whether the account exists. Anyone who controls the provider can impersonate its users after the second
  factor, so protect the provider as carefully as the panel.

## Terminal recordings access

- Recordings are available only with `recordings.view` at organization scope, through **Audit >
  Recordings**. API keys can never list or download them.
- Downloading a `.cast` is audited as `recording_download`. A recording can contain passwords or tokens
  printed on screen; treat it as sensitive data and store it accordingly.
- The panel has no built-in player. Use `asciinema play file.cast` locally. Retention defaults to 90 days
  and the chunks are encrypted with the secrets key.

## Agent updates

The core can order an agent to update itself (the order is typed and carries the SHA-256 of the package). The agent
downloads `/agent/talaia-agent_<arch>.deb` over its mutual-TLS channel, refuses an equal or older version and a
package whose hash differs, and installs it with `apt-get` in a separate systemd unit. This is code that runs as root
on every server, chosen by whoever controls the core: the hash protects against corruption and tampering in transit,
not against a compromised core, the same trust as the install command. Every order is audited as `agent_update`.

## What protects the panel and the agents

- **Sign-in:** password and TOTP, optional PIN on trusted devices, passkeys and OIDC for existing users, rate
  limiting, lock-out after repeated failures, sessions that expire, and protection of every change against
  requests from other sites (origin check).
- **Typed commands only.** The core never sends a shell command to an agent. It sends closed, validated
  messages (start a container, read logs, write a stack, open a terminal…), and the agent checks them again.
  Container names, stack names and paths are validated before they are used.
- **Destructive actions need an explicit confirmation** in the panel and in the request itself. Removing images,
  volumes and networks never forces: Docker refuses what is in use.
- **Agents connect outbound** over mutual TLS with a certificate from the core's own CA, bound to one server. An
  agent can only report about and receive commands for its own server. The agent port always presents the
  internal CA's certificate, even when the panel uses a public one, so whoever takes over your DNS cannot
  impersonate the core.
- **The certificate needs no DNS provider key** with the recommended setup: the zone delegates only
  `_acme-challenge.<name>` to the server, and `acme-dns` answers it on port 53 from its own read-only container,
  without the database, the secrets or the data of the core. It answers that name only, never recurses and limits
  each source network. See [dns-delegation.md](dns-delegation.md).
- **Everything the panel does is audited** with names, ids and hashes; the audit log never holds file contents,
  log text, the TOTP code, the journal search text, package lists or what is typed in a terminal. Denied
  authorization checks are recorded as `authz_denied`.
- **Scoped roles are enforced in the core.** The panel hides controls, but the API checks every route against
  the effective bindings; a hidden object returns 404 and a visible operation without permission returns 403.

## Terminal sessions: recording and retention

A terminal session always records, and cannot be opened if it cannot be recorded. This applies to container
terminals and to the host terminal.

- **What is recorded:** the *output* of the terminal and its resizing, in the asciicast v2 format. **What you
  type is not recorded**, but anything the shell echoes back is part of the output: a password typed in a prompt
  that echoes it, or printed by a command, ends up in the recording. The interface says so permanently.
- **Where:** in the core's database, split into chunks that are each encrypted with the key in
  `secrets.key` (volume `core-data`). Without that file the recordings cannot be read, and the same applies to
  backups of the database alone.
- **Retention:** 90 days by default. Change it with `TALAIA_TERMINAL_RECORDING_DAYS` (or
  `terminal_recording_days` in the configuration file). Older recordings are deleted when the core starts and
  every 24 hours.
- **Limits:** at most 4 sessions per server, shared by container and host shells; one session per user and
  container, and one host session per user and server, with at most 2 host sessions per agent; a session closes after 15 minutes
  without input or output, after 4 hours, or when its recording reaches 50 MiB. Each closure has a reason in the
  audit log (`user`, `idle`, `max_duration`, `recording_limit`, `slow_client`, `agent_offline`,
  `session_revoked`, `exited`).
- **Opening one** needs a full session and a one-time ticket that expires in 30 seconds and belongs to that
  browser session, and the connection must come from an allowed origin. Closing or revoking the panel session
  closes its terminals within a minute.

The panel has no built-in player. Users with `recordings.view` can list the sessions and download the
`.cast` file in **Audit > Recordings**; downloads are audited. To list and export them from the console,
run on the core machine, in `packaging/core`:

```bash
sudo docker compose run --rm -T core terminal list
sudo docker compose run --rm -T core terminal export <session-id> > session.cast
asciinema play session.cast
```

`terminal list` prints one line per session, newest first: id, opening time, state or closing reason, user,
container or host, duration and bytes recorded, plus `root-equivalent` for privileged containers. `terminal export`
decrypts a recording and writes the `.cast` file to the standard output.

## Whole-server actions (phase 3)

An administrator, or a server operator whose scope reaches the server, can change the whole machine, not
only its containers:

- **Package upgrades** run as root and can replace libraries, services and the kernel. Security-only or
  all-pending upgrades are available; they never remove packages and keep local configuration files. One
  apt upgrade or reboot can run at a time. The work runs in a transient systemd unit so it survives an
  agent restart, and the live log is shown in the panel.
- **Service control** starts, stops, restarts, reloads, enables or disables systemd services. Stop,
  restart and disable ask for confirmation; access-critical services show a stronger warning.
  `talaia-agent.service` cannot be stopped, restarted or disabled from the panel.
- **Reboot** needs the hostname the agent reports; the agent checks it again and schedules the restart.
  Talaia never powers a machine off.
- **The host terminal** opens a recorded root login shell on the server through a local PTY. It needs a
  confirmation and a fresh TOTP code, with the same replay guard as sign-in and password changes. It is
  limited to 2 sessions per agent and one per user and server, shares the 4-session per-server limit and
  the 15-minute idle, 4-hour and 50 MiB recording limits.

All of these are typed commands; the core does not send a shell line. The user guide, including safe
update and reboot steps, is [server.md](server.md).

### What phase 3 records and what it does not

The audit log records the action, actor, IP, result and identifiers such as the server, unit or job. The
phase 3 actions are `apt_status`, `apt_upgrade`, `host_reboot`, the `service_*` actions,
`journal_read`, `host_terminal_open` and `host_terminal_close`.

- **Never the TOTP code.** The code is checked and discarded; it is not written to the audit log,
  application logs or the recording.
- **Never the journal text or the search text.** `journal_read` stores the unit and boot, not the lines or
  the literal search.
- **Never package lists.** `apt_status` and `apt_upgrade` store the mode, flags and job id, not the
  package names or the command output.
- **Never keystrokes.** A terminal recording stores output and resizing; what you type is not stored, but
  anything the shell echoes back is part of the output. The TOTP prompt is not echoed into the recording.
- **Reading the audit log is not audited.** Otherwise every refresh of the Audit screen would add an event.

### Turning the host terminal off

Host terminals are enabled by default on Linux. To turn them off on one server, add this to
`/etc/talaia/agent.toml` and restart the agent:

```toml
[terminal]
host = false
```

```bash
sudo systemctl restart talaia-agent
```

The environment variable `TALAIA_AGENT_TERMINAL_HOST=false` has the same effect and takes precedence over
the file. The agent reports the capability as false, so the panel disables the button. Old agents also
report false and need an update.

## Reporting a vulnerability

Do not open a public issue: follow [SECURITY.md](../../SECURITY.md).
