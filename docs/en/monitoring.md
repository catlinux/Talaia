# Monitoring, alerts and checks

[Leer en castellano](../es/monitorizacion.md) · [Back to README](../../README.md)

This guide explains what Talaia measures, how metrics are stored and charted, how alerts and checks work,
where notifications go, and which outbound traffic is blocked. It is a phase 5 feature and is still in
progress. The related source of truth is [architecture.md](architecture.md); permissions are detailed in
[permissions.md](permissions.md) and the outbound/secret rules in [security.md](security.md).

## Contents

[Recording](#what-is-recorded) · [Resolution and retention](#resolutions-downsampling-and-retention) · [Charts](#charts-in-the-panel) · [Alerts](#alert-rules) · [Evaluation](#how-alerts-are-evaluated) · [Containers](#container-watching-and-server-reboots) · [Silences](#silences) · [Checks](#checks) · [Channels](#notification-channels) · [Outbound guard](#outbound-guard) · [Permissions](#who-can-do-what) · [API](#api-endpoints) · [Test](#test-it-step-by-step) · [Troubleshooting](#troubleshooting)

## What is recorded

The agent sends a host sample every **15 seconds**:

| Series | Meaning |
|---|---|
| CPU | Whole-machine CPU use, 0–100 % |
| Memory | Used and total bytes |
| Disk | Used and total bytes of the root filesystem (`/` on Linux, the system drive on Windows) |
| Network | Received and sent bytes on physical interfaces |

The core turns the network counters into receive/transmit **rates in bytes per second** by comparing two
consecutive samples; the first sample has no rate. Loopback, Docker, bridge, veth and VM interfaces are
excluded from the host network series.

The agent also samples every **running container** every 15 seconds: CPU percentage and used/limit memory.
Container memory subtracts the page cache Docker reports as inactive, so it reflects the working set more
closely. The agent sends its inventory every **60 seconds**, immediately after a Docker event, and a heartbeat every
10 seconds. It keeps no local metrics history; the core stores it.

## Resolutions, downsampling and retention

Samples are stored in three resolutions:

| Resolution | Source | Used for |
|---|---|---|
| `raw` | One row per agent sample | Ranges up to 6 hours |
| `5m` | Average and maximum of each 5-minute bucket | Ranges up to 7 days |
| `1h` | Average and maximum of each 1-hour bucket, built from `5m` | Ranges over 7 days |

Downsampling runs every **5 minutes**. A bucket is written only when it is complete. CPU, memory and disk
keep both an average and a maximum; network keeps an average. Tables are TimescaleDB hypertables, and old
chunks are dropped about a minute after startup and then every 24 hours.

The defaults are:

| Data | Raw | 5 minutes | 1 hour |
|---|---:|---:|---:|
| Server metrics | 48 hours | 30 days | 400 days |
| Container metrics | 24 hours | 7 days | 90 days |

They can be changed in the core TOML file (`/etc/talaia/core.toml`, or the file named by `TALAIA_CONFIG`).
The packaged Compose stack passes its configuration through environment variables and does not mount a
TOML file, so these defaults apply unless you provide one.

```toml
[metrics]
server_raw_hours = 48
server_5m_days = 30
server_1h_days = 400
container_raw_hours = 24
container_5m_days = 7
container_1h_days = 90
```

Hours accept 1–87600 and days 1–3650. The values are read when the core starts.

## Charts in the panel

Open a server and scroll to **History**. The four charts show CPU, memory, disk and network. The range
selector offers **1 hour, 6 hours, 24 hours, 7 days and 30 days**; the panel refreshes the data every
30 seconds. Memory and disk are drawn as a percentage of their totals.

## Alert rules

Talaia ships six built-in rules:

| Rule | Type | Severity | Default behaviour |
|---|---|---|---|
| Server offline | `server_offline` | Critical | The agent is disconnected and its last contact was at least 45 seconds ago |
| Disk usage high | `disk_high` | Critical | The root filesystem is at or above 90 % |
| Container down | `container_down` | Critical | A watched container has not been running for 20 seconds |
| Container unhealthy | `container_unhealthy` | Warning | A container health check has been unhealthy for 60 seconds |
| Health check failed | `check_failed` | Critical | A check fails twice in a row |
| Certificate expiring | `cert_expiry` | Warning | A check certificate expires in 14 days or less |

Built-in rules can be edited and disabled, but not deleted. Talaia does not create CPU or memory rules by
default; add them from **Settings > Alerts > Rules** if you need them.

A rule has a type, a severity (`warning` or `critical`), a scope, parameters, an optional repeat interval
and a channel list. The scope can be **organization**, **group**, **server**, **stack** or **container**;
the panel uses the existing server and group pickers, and asks for a stack or container name when needed.
If the channel list is empty, the rule uses **every active channel**.

These are the rule types and their parameters (the same ranges the core validates):

| Type | Parameters | Range |
|---|---|---|
| `server_offline` | `seconds` | 0–86400 |
| `cpu_high` | `threshold` (%), `for_seconds` | 1–100, 0–86400 |
| `mem_high` | `threshold` (%), `for_seconds` | 1–100, 0–86400 |
| `disk_high` | `threshold` (%), `for_seconds` | 1–100, 0–86400 |
| `container_down` | `seconds` | 0–86400 |
| `container_unhealthy` | `seconds` | 0–86400 |
| `check_failed` | `consecutive_failures` | 1–20 |
| `cert_expiry` | `days` | 1–365 |

The repeat interval is 0–604800 seconds. The panel offers **never, 5 minutes, 15 minutes, 1 hour, 6 hours
and 24 hours**. A value of 0 means the alert is notified once when it fires and once when it resolves.

## How alerts are evaluated

The alert engine evaluates every **5 seconds**, and immediately when a server inventory changes or a check
finishes. Its state comes from the latest agent snapshot and the latest check results.

- **Pending duration.** CPU, memory, disk, container and offline rules can require the condition to hold
  for a configured time. The timer starts at the first failing evaluation; the alert opens only after the
  whole duration has passed.
- **Hysteresis.** Once a CPU, memory or disk alert is firing, it keeps firing until the value falls at least
  **2 points below** the threshold. A 90 % rule therefore clears below 88 %.
- **Cooldown.** If the same rule and subject fire again within **10 minutes** of the last notification,
  Talaia records the incident but suppresses the new firing notification. This prevents flapping from
  flooding a channel.
- **Repeat.** While an alert is firing, is not acknowledged and has a repeat interval, Talaia sends another
  firing notification when the interval has elapsed.
- **Acknowledge.** Acknowledging stops the repeat notifications and adds the “Acknowledged by …” marker.
  It does not resolve the incident; Talaia resolves it when the condition clears.
- **Startup grace.** `server_offline` rules are not evaluated during the first **90 seconds** after the core
  starts. Other rule types can fire immediately.
- **Silenced incidents.** A silence suppresses delivery, not detection: the alert is still opened and
  stored, and the panel marks it as silenced.

## Container watching and server reboots

Talaia does not monitor every container name it has ever seen. It watches a container only after it has
observed it **running**. A container stopped, brought down or removed **through Talaia** is deliberately
unwatched, so the action you requested does not raise an alert:

- stopping or removing a container in the panel;
- bringing down, removing or forgetting a stack.

A container stopped outside Talaia remains watched and raises `container_down` after its configured
seconds. If it starts again, the alert resolves and the watch continues.

A **server reboot started from Talaia** creates a server silence for **10 minutes** with reason `reboot`.
The silence suppresses alerts while the machine
restarts; it is visible in the Silences tab.

### How fast is a stopped container noticed?

Docker events wake the agent immediately. The agent batches event-driven inventories for **2 seconds** and
sends one; the core evaluates that server as soon as it arrives, in addition to its 5-second ticker. A
default `container_down` rule waits 20 seconds, and the notification worker polls the delivery queue every
**5 seconds**. A stopped container with a working channel is therefore normally reported in well under a
minute. A disk crossing 90 % has no pending delay, so it is reported after the next sample and evaluation,
also normally within a minute.

## Silences

A silence suppresses notifications for a scope and, optionally, one rule type. It can be created from an
alert; the Silences tab lists and deletes them. The API enforces these limits:

- **30 days maximum**; the panel offers 30 minutes, 1 hour, 4 hours, 24 hours, 7 days and a custom number of
  minutes up to 43200;
- **reason required**, up to 200 characters;
- scope **organization, group, server, stack or container**.

Talaia still records the incident and keeps it in the alert history. Only delivery is suppressed. A silence
does not acknowledge an alert. Deleting a silence does not send what was suppressed while it lasted; later repeats and the resolution of the incident are delivered normally.

## Checks

Checks run from the core, not from the agent. The optional **server association** ties the check's alerts to
that server and its scope; it does not run the probe on the agent.

| Kind | Target | What it does |
|---|---|---|
| HTTP | Full `http://` or `https://` URL | Sends `GET` or `HEAD`, follows up to three redirects, reads at most 64 KiB of the body and compares the status |
| TCP | `host:port` | Opens a guarded TCP connection |
| TLS | `host:port` | Opens TCP, performs a TLS handshake with TLS 1.2 or newer and validates the certificate |

HTTP options are the method (`GET` or `HEAD`), the expected codes (comma-separated single codes or ranges,
for example `200,301-399`) and a timeout of **1–10 seconds**. The default is `GET`, `200-399` and 10 seconds.
Each redirect is checked again by the outbound guard. Redirect loops and more than three redirects fail the
check.

The interval is **30 seconds minimum** and 24 hours maximum. Talaia stores the time, success/failure,
latency in milliseconds, HTTP status code (for HTTP), a short error and the certificate expiry date (when
the response or handshake exposed one). It **never stores the HTTP response body**, request credentials or
a copy of the target's content. Results older than 30 days are deleted; a cleanup runs at startup and then
once a day.

A check can be disabled, edited and deleted. **Run now** executes it immediately, stores the result and
shows it in the panel. The `cert_expiry` rule evaluates the `cert_not_after` value; it does not perform a
separate scan.

## Notification channels

Configure channels in **Settings > Alerts > Channels**. Every channel has a name, a type, an enabled switch
and a language (`ca`, `es` or `en`) that controls the message text. A channel can be tested in place: the
test is sent immediately and its success or redacted error is shown.

| Type | Configuration | Secret |
|---|---|---|
| Telegram | Chat ID | Bot token |
| ntfy | Server URL (default `https://ntfy.sh`), priority 1–5 | Topic; optional access token |
| Email | SMTP host, port, from address, recipient list, STARTTLS or implicit TLS | Username and password |
| Discord | Nothing else | Discord webhook URL |
| Webhook | Nothing else | Endpoint URL; optional HMAC secret |

Email **never sends plain text**: only STARTTLS and implicit TLS are accepted. Discord only accepts an
official `discord.com` or `discordapp.com` webhook URL under `/api/webhooks/`. ntfy sends the title,
priority and severity tags, and uses a bearer token when one is configured.

Firing and resolved notifications are queued. A failed delivery is retried after **30 seconds, 2 minutes,
5 minutes and 15 minutes**, for at most five attempts. The worker checks the queue every 5 seconds. The
`last status` column shows the last successful date or the last error, and the error is redacted before it is
stored or shown.

### Webhook signature

A webhook sends a JSON object with `version`, `event`, `alert` and `sent_at`. If an HMAC secret is set, every
request carries:

- `X-Talaia-Timestamp`: Unix seconds;
- `X-Talaia-Signature`: `sha256=<hex HMAC-SHA256>`.

The signed bytes are the timestamp, a literal dot and the **exact raw request body**:

```text
HMAC-SHA256(secret, timestamp + "." + body)
```

Verify it before parsing or trusting the JSON. This Python example uses the standard library:

```python
import hashlib
import hmac

secret = b"replace-with-the-channel-secret"
timestamp = headers["X-Talaia-Timestamp"]
body = raw_request_body  # exact bytes read from the request
expected = hmac.new(
    secret,
    timestamp.encode("ascii") + b"." + body,
    hashlib.sha256,
).hexdigest()
received = headers["X-Talaia-Signature"].removeprefix("sha256=")
if not hmac.compare_digest(expected, received):
    raise SystemExit("invalid signature")
```

Do not reserialize the JSON before checking the signature: whitespace or key order changes would invalidate
it. The signature header is absent when the channel has no HMAC secret.

### Secrets

The API returns `has_secret`, never the secret itself. In edit mode the panel shows **Secret saved** and a
**Replace** action; fields stay empty until you replace them. Saving without replacing sends no secret
field, so the stored one is kept. Saving with replacement sends only the new value. Secret inputs use
`type="password"` and `autocomplete="new-password"`.

Channel secrets are encrypted with AES-256-GCM using the same `data_dir/secrets.key` as TOTP and the OIDC
client secret. Errors are redacted against every secret value before they are stored in the channel status
or returned to the panel.

## Outbound guard

Checks and notification channels can make the core connect to arbitrary URLs. Talaia validates the
destination **after DNS resolution, at connection time**, so a DNS-rebinding answer cannot bypass the
policy. HTTP redirects and every resolved address are checked again. URL credentials are rejected and only
`http` and `https` are accepted for HTTP targets; alternate numeric IPv4 spellings are rejected.

By default the guard blocks:

- loopback, RFC 1918 private addresses, carrier-grade NAT (`100.64.0.0/10`), link-local and IPv6 unique
  local addresses;
- benchmark, documentation and reserved ranges;
- multicast, unspecified and broadcast addresses;
- cloud metadata addresses.

The **Allow checks and notifications to reach private networks** switch in **Settings > Alerts** relaxes the
private, loopback and CGNAT restrictions. It does **not** relax link-local addresses, the reserved
documentation ranges, multicast/broadcast or cloud metadata. `169.254.169.254` and `fd00:ec2::254` remain
blocked whatever the switch says, because they can expose instance credentials and cloud control data. Leave the switch off unless the core and the targets are on a trusted
network.

## Who can do what

| Role | Alerts capability |
|---|---|
| Administrator (`admin`) | View, acknowledge/silence and manage rules, checks, channels and settings |
| Server operator (`server_operator`) | View and silence alerts in its scope |
| App operator (`app_operator`) | View and silence alerts in its scope |
| Viewer (`viewer`) | View alerts in its scope |
| Metrics only (`metrics_only`) | View alerts in its scope |

The permissions are:

| Permission | Minimum scope | Allows |
|---|---|---|
| `alerts.view` | Container | See alerts, history, summaries and silences in scope |
| `alerts.silence` | Stack | Acknowledge, create/delete silences and stop watching a container |
| `alerts.manage` | Organization | Rules, checks, channels, test sends and the private-target setting |

Only `admin` has `alerts.manage`; the **Settings > Alerts** section is hidden for everybody else. API keys
can use `alerts.view` and `alerts.silence`, but never `alerts.manage`: the management routes reject API keys
even when their role bindings would allow them.

## API endpoints

All paths are under `/api/v1`. Responses are JSON; writes require a session, an allowed `Origin` and the
usual CSRF rules unless an API key is accepted.

| Method and path | Purpose | Permission |
|---|---|---|
| `GET /servers/{id}/metrics` | Server history; `range=1h\|6h\|24h\|7d\|30d`, or `from`/`to` | `server.view` |
| `GET /servers/{id}/containers/{name}/metrics` | Container history; same ranges | `container.view` |
| `GET /alerts` | Active or history list; `state`, `limit`, `offset`, `server_id` | `alerts.view` on each target |
| `GET /alerts/summary` | Counts and highest severity per server | `alerts.view` on each target |
| `POST /alerts/{id}/ack` | Acknowledge | `alerts.silence` |
| `POST /alerts/{id}/unwatch` | Stop watching a container alert | `alerts.silence` |
| `GET /silences` | Active silences | `alerts.view` |
| `POST /silences` | Create a silence | `alerts.silence` |
| `DELETE /silences/{id}` | Delete a silence | `alerts.silence` |
| `GET /alert-rules` | List rules | `alerts.view` at organization |
| `POST /alert-rules` | Create a rule | `alerts.manage` at organization |
| `PATCH /alert-rules/{id}` | Edit or disable a rule | `alerts.manage` at organization |
| `DELETE /alert-rules/{id}` | Delete a non-built-in rule | `alerts.manage` at organization |
| `GET /checks` | List checks and their latest result | `alerts.view` at organization |
| `POST /checks` | Create a check | `alerts.manage` at organization |
| `PATCH /checks/{id}` | Edit a check | `alerts.manage` at organization |
| `DELETE /checks/{id}` | Delete a check | `alerts.manage` at organization |
| `POST /checks/{id}/run` | Run now and store the result | `alerts.manage` at organization |
| `GET /channels` | List channels without secrets | `alerts.manage` at organization |
| `POST /channels` | Create a channel | `alerts.manage` at organization |
| `PATCH /channels/{id}` | Edit a channel; omit `secret` to keep it | `alerts.manage` at organization |
| `DELETE /channels/{id}` | Delete a channel | `alerts.manage` at organization |
| `POST /channels/{id}/test` | Send a test message | `alerts.manage` at organization |
| `GET /settings/alerts` | Read `allow_private_targets` | `alerts.manage` at organization |
| `PUT /settings/alerts` | Change `allow_private_targets` | `alerts.manage` at organization |

The rule, check, channel and settings writes are marked **no API key**: an API key cannot use them even if
it has `alerts.manage`.

## Test it step by step

Use a test server and a channel whose test button already works.

### Stop a disposable container

1. Create a container that does nothing:

   ```bash
   docker run -d --name talaia-alert-test busybox sleep 3600
   ```

2. Wait about 10 seconds for Talaia to see it running and record the watch.
3. Stop it **from the server shell**, not from the Talaia panel, because a stop requested in the panel is
   deliberately unwatched:

   ```bash
   docker stop talaia-alert-test
   ```

4. With the default `container_down` rule (20 seconds), the alert and its notification should arrive in under
   a minute. Check **Alerts > Active** and the channel's last status.
5. Start it again to resolve the alert, then remove it:

   ```bash
   docker start talaia-alert-test
   docker rm -f talaia-alert-test
   ```

### Fill the root disk safely

Run this only on a disposable test server. It temporarily brings the root filesystem to about 91 %, just
above the default rule, and removes the file when the shell exits:

```bash
file=/talaia-disk-alert-test
trap 'sudo rm -f "$file"' EXIT
total=$(df --output=size --block-size=1 / | awk 'NR==2 {print $1}')
used=$(df --output=used --block-size=1 / | awk 'NR==2 {print $1}')
size=$(( total * 91 / 100 - used + 100 * 1024 * 1024 ))
if [ "$size" -le 0 ]; then
  echo "The disk is already at or above 91%; nothing to do."
elif [ "$(( total - used - size ))" -lt 1073741824 ]; then
  echo "Refusing to leave less than 1 GiB free."
else
  sudo fallocate -l "$size" "$file"
fi
```

Wait for the disk alert and then press Ctrl-D (or run the `rm` command) to remove the file and let the alert
resolve. If `fallocate` is unavailable, stop here rather than filling a production disk with `dd`. Keep at
least 1 GiB free for the core and PostgreSQL.

## Troubleshooting

**No alert arrives**

1. Open **Alerts > Active** and check that the incident exists. If it does not, the rule is disabled, its
   scope does not match, the condition is not true yet, or the pending duration has not elapsed.
2. Open **Settings > Alerts > Channels** and read the last status. Use **Send test**; a redacted error is
   shown there.
3. Check the rule's channel list. An empty list means all active channels; a non-empty list excludes every
   channel not selected.
4. Check **Silences**; an active silence suppresses delivery but not the incident.
5. If the target is on a private address, the outbound guard returns `blocked address` or `connection failed`.
   Enable **Allow checks and notifications to reach private networks** only if that is intended; metadata and
   link-local addresses stay blocked.
6. For container alerts, confirm the container was seen running. Containers stopped or removed through
   Talaia are unwatched by design.
7. For `server_offline`, wait past the 90-second startup grace. For `check_failed`, confirm the check is
   enabled and its latest result is failing.

**The core fails at startup with a migration error**

Talaia requires PostgreSQL with the **TimescaleDB extension**. Migration `0014_metrics.sql` runs
`CREATE EXTENSION IF NOT EXISTS timescaledb`; a plain PostgreSQL server answers with an error such as
`extension "timescaledb" is not available` (and a missing `timescaledb.control` file). The packaged
`packaging/core/compose.yaml` uses `timescale/timescaledb:latest-pg16`, which already includes it. Check:

```bash
sudo docker compose logs --tail=100 db core
```

Do not point the core at a plain PostgreSQL image and expect metrics to work.


