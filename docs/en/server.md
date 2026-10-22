# The server screen

[Leer en castellano](../es/servidor.md) · [Back to README](../../README.md)

The server screen manages one whole VPS, not only the containers that run on it. It has tabs for the
**Overview**, **Containers**, **Services**, **Logs** and **Updates**, plus a **Terminal** button for a root
shell on the server itself.

The administrator account is equivalent to root on every managed server. Read
[security.md](security.md) before you use the host terminal, upgrade packages or change services. The
implementation details are in [architecture.md](architecture.md); the installation steps are in
[installation.md](installation.md).

## Before you start

- Sign in with a full session. The panel needs the second factor; a pending session cannot use these
  operations.
- Keep an out-of-band way into the server (the provider console or SSH) until you have finished. A wrong
  network or SSH change can remove the panel's access.
- The server must run `talaia-agent` with the tools for the feature you want: `apt-get` and `apt-mark` for
  updates, `systemctl` and `systemd-run` for services and transient apt units, `journalctl` for logs, and
  `bash` or `sh` for the host terminal. The agent is Linux-only for the host terminal.
- An old agent reports fewer capabilities. The panel shows a button as disabled and explains what is
  missing instead of sending an unsupported command.

## The header

The header always shows the server name and, when the agent is connected, a summary of the containers.
It also shows the agent version, an **Update agent** button when a newer package is available, and the
**Terminal** button.

The **Terminal** button opens the root host shell, not a container shell. It is disabled when the agent
has host terminals turned off or is too old. The confirmation, TOTP code, recording notice and red
`Root on <server>` banner are described in [The host terminal](#the-host-terminal).

## Overview

The **Overview** tab is the landing page. It shows three gauges for CPU, memory and disk, followed by a
facts list with the operating system, kernel, architecture, CPU count, memory, network counters,
containers, services, agent version and Docker version when known. Uptime appears when the agent reports
it.

The **Needs attention** section is only shown when there is something to do. It links to the tab that
fixes the item:

- a reboot is required, or security updates are pending: **Updates**;
- the disk is above 85%: **Containers**, where the Docker resource cleanup lives;
- failed systemd services: **Services**;
- a newer agent package: **Updates**.

The list is a shortcut, not a replacement for reading the tab: always check what will change before you
confirm an action.

## Containers

The **Containers** tab keeps the container and stack management of the previous phase. It shows the
containers of the server, the Compose stacks Talaia manages, and the Docker images, volumes and networks
that can be cleaned up. Container logs and the container terminal are still available here.

## Services

The **Services** tab lists the systemd **services** the agent reported, with their active state and
whether their unit file is enabled. Use the search and the state filters to find one.

The available actions are:

- **Start**, **Reload** and **Enable** without an extra confirmation;
- **Stop**, **Restart** and **Disable** after a confirmation dialog.

Stopping, restarting or disabling an access-critical service shows an additional warning. The fixed list
is `ssh.service`, `sshd.service`, `docker.service`, `networking.service`, `systemd-networkd.service` and
`NetworkManager.service`. If you lose the panel connection after changing one of them, use the provider
console or SSH to recover.

`talaia-agent.service` cannot be stopped, restarted or disabled from the panel: that would cut the agent
from the core. Talaia updates the agent through **Update agent** instead. Static, masked and alias units
cannot be enabled or disabled here.

Services are only `.service` units in this phase; timers and sockets are not managed as services.

## Logs

The **Logs** tab reads the systemd journal of the server. You can filter by:

- **Unit**: the services the agent reported. Leave it empty for every unit.
- **Priority**: all, errors, warnings, notices, info or debug. It passes a maximum journal priority
  (0–7).
- **Boot**: all boots, the current boot or the previous boot.
- **Time range**: all, the last 15 minutes, 1 hour, 24 hours, 7 days or a custom RFC 3339 range.
- **Search**: literal text, up to 200 characters. It is a text search, not a regular expression.
- **Lines**: 100, 200, 500 or 1000 entries. The default is 200.

Press **Search** to apply the filters. **Follow** asks for new entries every 3 seconds using the journal
cursor; the panel keeps the newest 2000 entries. **Download** saves the entries currently on screen as a
plain text file with time, unit, priority and message.

The journal text and the search text are not written to the audit log. The audit entry `journal_read`
records the unit and boot, not the lines.

## Updates

The **Updates** tab shows the pending apt packages. Security updates are marked and sorted first. It
also shows:

- the last time the agent checked and the last time the package lists were updated;
- whether a reboot is required, with the reasons (the `/run/reboot-required` flag or a newer installed
  kernel than the running one);
- held packages and the package names that would be upgraded;
- a warning when the update includes Docker packages that will restart the containers.

**Check now** runs `apt-get update` and then asks the agent for a fresh list. Without it, the tab shows
the last summary the agent has. A refresh can take longer than a normal read.

### Updating packages safely

1. Open **Updates** and press **Check now** once. Read the package list, the security marks, the held
   packages and any Docker warning.
2. Choose **Update security (N)** to install only packages whose candidate comes from a security archive,
   or **Update all (N)** to upgrade all pending packages.
3. Confirm the dialog. The agent computes the package list itself for a security update; the core never
   sends package names.
4. Keep the tab open to follow the live log. The job belongs to the core and the agent, not to the
   browser; leaving the screen only hides the live log.

The upgrade never removes packages (`--no-remove`), keeps local configuration files (`--force-confdef`
and `--force-confold`) and uses a 120-second lock timeout. It runs in a transient systemd unit named
`talaia-apt-<short job id>`, so it survives an agent restart; when the agent starts again it resumes the
unit and reports the result. State and logs live under `/var/lib/talaia/apt/` on the server.

Only one apt upgrade or reboot can run at a time. The core refuses a new one with
**Another apt operation or reboot is already running** while the previous is still active. Wait for it and
read its log instead of retrying.

If a reboot is required after the upgrade, use the reboot flow below. Some changes (a new kernel or a
service library) only take effect after the restart.

## Rebooting the server

The reboot is intentionally deliberate: there is no one-click power control.

1. Open **Updates** and press **Reboot** (or the button in the reboot notice).
2. Type the **hostname the agent reports** in the dialog. The panel enables the confirmation only when it
   matches, ignoring case and surrounding spaces.
3. Press the confirmation button.

The agent checks the hostname again, refuses the request while an apt upgrade is running, and schedules
`systemctl reboot` five seconds later. It never powers the machine off: a powered-off server could not be
switched on from Talaia. The panel loses the agent connection while the server restarts; the screen goes
offline and comes back when the agent reconnects. Keep the provider console available for a server that
does not return.

## The host terminal

The **Terminal** button in the header opens a root login shell on the server through a local PTY. This is
the whole machine, not a container. The panel shows a permanent red **Root on <server>** banner and the
recording mark while the session is live.

To open it, the agent must report host-terminal support (the button is disabled otherwise), and you must:

1. confirm the dialog, and
2. type a current six-digit TOTP code. The code uses the same replay guard as sign-in and password
   changes: a code that has already been used is refused.

Choose `bash` or `sh`. The agent starts `/bin/bash -l` or `/bin/sh -l` as root with a clean environment,
`HOME=/root`, the standard `PATH`, the system `LANG`, and the working directory `/root`. Closing the
session kills its process group. The list of shells is closed; the panel cannot send an arbitrary shell.

The session is recorded like a container terminal, but only the **output** and resizing are stored;
**keystrokes are not recorded**. Anything the shell echoes, including a password typed at an echoing
prompt or printed by a command, becomes part of the recording. The session is audited as
`host_terminal_open` and `host_terminal_close`; the TOTP code is never written to the audit log or the
logs.

Limits:

- at most 2 host sessions per agent, and one host session per user and server;
- at most 4 terminal sessions in total per server, shared with container terminals;
- a session closes after 15 minutes without input or output, after 4 hours, or when its recording reaches
  50 MiB. The reason is recorded.

The recording is stored encrypted in the core database with retention configured like container
recordings. The panel has no recording player yet; `terminal list` and `terminal export` on the core
machine work for both container and host sessions, and show a host session as `host`.

### Turning the host terminal off

Host terminals are enabled by default on Linux. To disable new sessions on one server, edit
`/etc/talaia/agent.toml`:

```toml
[terminal]
host = false
```

Then restart the agent:

```bash
sudo systemctl restart talaia-agent
```

The environment variable `TALAIA_AGENT_TERMINAL_HOST` also accepts `true` or `false`; it takes precedence
over the file. With systemd, add it in a drop-in if your install does not already set it:

```bash
sudo systemctl edit talaia-agent
```

```ini
[Service]
Environment=TALAIA_AGENT_TERMINAL_HOST=false
```

Then run `sudo systemctl daemon-reload && sudo systemctl restart talaia-agent`.

The agent reports the new value in its inventory. The panel disables the button with an explanation when
host terminals are off; old agents report `false` and need an update. Turning the option off affects new
sessions; use your normal session policy for sessions that are already open.

### When to use it

Use the host terminal for a change that is not tied to one container and that you can safely make in a
recorded session: inspecting a service, checking a network file, or running a one-off command. Prefer
SSH or the provider console when the panel itself is the thing you are changing, when the agent is
offline, or when you need a recovery path that does not depend on the panel. The host terminal is a
convenience, not a replacement for out-of-band access.

## Reading the audit log

**Audit** is in the sidebar. It is a read-only list of the events the core recorded, newest first. Each
row shows the local time and a relative time, the actor, the action (translated when the panel knows it,
otherwise the raw action name), the result, and the server name or id. **Details** expands a few detail
keys as text; detail values are never rendered as HTML.

Filters:

- **Action**: all actions, sign-in, servers, containers and stacks, stacks, updates, services, journal,
  terminal, host terminal, or host.
- **Actor**: exact username. Type it and press **Filter** (or Enter).

**Load more** follows the cursor returned by the core. The core accepts a page size from 1 to 200 and uses
50 by default; the screen asks for 50. Reading this screen is not audited: it would flood the log.

The main phase 3 actions are `apt_status`, `apt_upgrade`, `host_reboot`, `service_start`,
`service_stop`, `service_restart`, `service_reload`, `service_enable`, `service_disable`,
`journal_read`, `host_terminal_open` and `host_terminal_close`. Container and stack actions keep their
`container_*` and `stack_*` names. The audit log never stores the TOTP code, journal lines, the journal
search text, package lists or terminal keystrokes.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| An update fails with an apt lock error | Another apt process holds the lock. Wait for `apt-daily`, `unattended-upgrades` or a manual apt command to finish. Check the holder with `sudo fuser -v /var/lib/dpkg/lock-frontend` and `sudo fuser -v /var/lib/apt/lists/lock`; do not delete lock files. The agent waits up to 120 seconds for the lock. |
| **Another apt operation or reboot is already running** | A previous upgrade or reboot is still active. Wait for it and read the live log or the unit shown in the panel; `systemctl list-units 'talaia-apt-*' --all` lists the transient units. The core will not start a second apt upgrade or reboot. |
| The **Terminal** button is disabled | The agent has `[terminal] host = false`, the `TALAIA_AGENT_TERMINAL_HOST` environment is false, or the agent is too old to report host-terminal support. Check `/etc/talaia/agent.toml`, `systemctl cat talaia-agent`, and the agent version; update the agent and restart it. |
| A host terminal opens and then closes immediately | Check the live agent log with `sudo journalctl -u talaia-agent -n 50 --no-pager`. A missing shell, a rejected TOTP code, the session limits or an agent disconnect are recorded with a reason. |
| The **Services** tab says the agent is offline | The core has no live agent stream for that server. Check `sudo systemctl status talaia-agent --no-pager` and the network path to the core. |
| A service action is refused | The unit is not in the last inventory, the action needs confirmation, or it is `talaia-agent.service` with stop/restart/disable. Refresh the list and look at the message. |
| The journal shows no entries | Clear the unit and search filters and try a wider time range. Confirm `journalctl` works on the server and that the agent can read the journal. |
| A reboot was accepted but the server does not return | The panel is already disconnected. Use the provider console or SSH and check `systemctl status talaia-agent` and the boot messages. The panel cannot power a machine on. |
| The audit screen is empty | Widen or clear the action and actor filters. Reading the audit log is not itself audited, so an empty read does not create an event. |

For installation problems, including enrollment and certificates, see
[installation.md](installation.md). For what the panel can do as root and what is recorded, see
[security.md](security.md).
