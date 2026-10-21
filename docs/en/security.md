# Security

[Leer en castellano](../es/seguridad.md) · [Back to README](../../README.md)

What the panel can do to your servers, what protects it, and what is recorded. The design behind it is in
[architecture.md](architecture.md); the installation steps are in [installation.md](installation.md).

## The administrator is root on every managed server

In this version there is one role: the administrator, protected by a password and a second factor (TOTP). **That
account is equivalent to root on every server it manages.** This is by design, not an oversight, because several
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
- **Updates from the panel** are applied by a root helper on the core machine ([installation.md](installation.md),
  section 5). The core can only ask for a commit of `origin/main` that is newer than the running one.

Keep that account safe: a long, unique password, an authenticator you control, and no sharing. Per-user roles
and scopes (for example, access to a single stack) come in a later phase; when they do, the permissions to edit
stacks, adopt projects and open terminals will be marked as equivalent to root and will not be granted by
default.

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

Both methods are additive: the password, TOTP and recovery codes remain the base and the recovery path. Setup,
use, removal and troubleshooting: [access.md](access.md).

## What protects the panel and the agents

- **Sign-in:** password and TOTP, optional PIN on trusted devices and passkeys, rate limiting, lock-out after
  repeated failures, sessions that expire, and protection of every change against requests from other sites
  (origin check).
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
  log text or what is typed in a terminal.

## Terminal sessions: recording and retention

A terminal session always records, and cannot be opened if it cannot be recorded.

- **What is recorded:** the *output* of the terminal and its resizing, in the asciicast v2 format. **What you
  type is not recorded**, but anything the shell echoes back is part of the output: a password typed in a prompt
  that echoes it, or printed by a command, ends up in the recording. The interface says so permanently.
- **Where:** in the core's database, split into chunks that are each encrypted with the key in
  `secrets.key` (volume `core-data`). Without that file the recordings cannot be read, and the same applies to
  backups of the database alone.
- **Retention:** 90 days by default. Change it with `TALAIA_TERMINAL_RECORDING_DAYS` (or
  `terminal_recording_days` in the configuration file). Older recordings are deleted when the core starts and
  every 24 hours.
- **Limits:** at most 4 sessions per server and one per user and container; a session closes after 15 minutes
  without input or output, after 4 hours, or when its recording reaches 50 MiB. Each closure has a reason in the
  audit log (`user`, `idle`, `max_duration`, `recording_limit`, `slow_client`, `agent_offline`,
  `session_revoked`, `exited`).
- **Opening one** needs a full session and a one-time ticket that expires in 30 seconds and belongs to that
  browser session, and the connection must come from an allowed origin. Closing or revoking the panel session
  closes its terminals within a minute.

The panel has no recording player yet. To list and play them, run on the core machine, in `packaging/core`:

```bash
sudo docker compose run --rm -T core terminal list
sudo docker compose run --rm -T core terminal export <session-id> > session.cast
asciinema play session.cast
```

`terminal list` prints one line per session, newest first: id, opening time, state or closing reason, user,
container, duration and bytes recorded, plus `root-equivalent` for privileged containers. `terminal export`
decrypts a recording and writes the `.cast` file to the standard output.

## Reporting a vulnerability

Do not open a public issue: follow [SECURITY.md](../../SECURITY.md).
