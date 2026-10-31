# Web panel guide

[Leer en castellano](../es/panel.md) · [Back to README](../../README.md)

This guide explains the Talaia web panel: how it is organised, what each screen does and how to
reach the actions you need. For installation and sign-in, see [installation.md](installation.md).
For metrics, alert rules and notification channels, see [monitoring.md](monitoring.md).

## Contents

[Layout](#layout) · [Overview](#overview) · [Servers](#servers) · [Server page](#server-page) ·
[Containers and Stacks](#containers-and-stacks) · [What is this? tips](#what-is-this-tips) ·
[Catalogue](#catalogue) · [Alerts](#alerts) · [Command palette](#command-palette) · [Users, Audit and Settings](#users-audit-and-settings) ·
[Mobile](#mobile)

## Layout

The sidebar is grouped into **Infrastructure**, **Monitoring** and **Administration**. **Overview** is
the home screen; **Servers**, **Containers** and **Stacks** are under Infrastructure; **Alerts** and
**Audit** under Monitoring; **Users** and **Settings** under Administration. Some entries only appear
when your account has access to that area.

The top bar shows breadcrumbs for the current page, a **Search…** box that opens the command palette,
the alert bell and the user menu. The user menu changes the **Language** and **Theme**, opens
**About** and signs out. The sidebar can be collapsed on a computer; a phone opens it as an overlay.

Every screen and tab has its own address. Back, reload and shared links keep the same screen open, and
the panel checks permissions again when the address is opened.

## Overview

**Overview** is the fleet at a glance. The top strip summarises **Servers**, **Alerts**, **Security
updates** and **Reboots pending**. Below it, **Needs attention** lists the things that should be dealt
with first, with one action per item:

- **Open alert** opens the alert detail panel for an active critical or warning alert.
- **Open server** opens a server that is offline or has a full disk.
- **View updates** opens the **Updates** tab of a server with pending security updates or a required
  reboot.
- **Update agent** opens the server whose agent has an update available.

The same screen has a compact server table and the recent activity of the fleet. If your account can
enroll servers, **Add server** is also in the page header.

## Servers

**Servers** is the fleet table, with an optional **Cards** view. The search box and the filters
(**Status**, **Tag**, **Security updates**, **Reboot required**) and the table sort are kept in the
address, so a filtered or sorted list can be shared or reloaded. At narrower widths secondary columns
such as OS and updates are hidden so the **Agent** column and the row menu stay visible.

Each row has its status, name and hostname, CPU, RAM and disk meters, containers, alerts, updates and
agent version. The row menu opens the server, its containers, its updates or its alerts. A server with
limited access shows only what its permissions allow.

To add a server, press **Add server**, give it a name and press **Create token**. Run the command shown
on the new server within 15 minutes: the token works once and is not shown again. The server appears in
the list after its agent enrolls. See [installation.md](installation.md#4-add-a-server).

## Server page

Opening a server shows a fixed header with its name, status, CPU, RAM and disk use and its main
actions. **View alerts** opens the alert inbox filtered by that server; **Copy server ID** copies the
identifier used by the API and the CLI. If the agent can be updated, the header offers the agent
update action. **Terminal** opens the recorded root shell when the agent allows it.

The tabs are **Overview**, **Containers**, **Services**, **Logs**, **Updates** and **Alerts**. The
header and tabs stay in place while the selected tab scrolls. Each tab is also a direct address, for
example `/servers/<id>/updates`, and a tab is hidden when your account cannot use it.

## Containers and Stacks

**Containers** and **Stacks** are fleet-wide lists across all servers. Both offer search, server and
state filters, grouping and a table with the important facts. The container list also shows CPU and
memory use; the stack list shows how many containers are running.

Depending on the row, its menu offers actions such as **Start**, **Stop**, **Restart**, **Remove**,
**Logs** or **Terminal**. Select several rows to use the bulk actions. Dangerous actions ask for confirmation:
removing a container keeps its volumes, stopping a stack stops its containers, and the confirmation
explains the effect before you continue. Adopting a running Compose project from **Not managed** uses
**Adopt**; Talaia manages the project where it is, without copying or restarting it.

Inside a server, **Containers** also lists managed stacks. A stack has its own page with its containers,
logs and actions, and **New stack** opens the Compose editor. A Compose file runs as root on the
server, so only give access to people you trust with the server.

## What is this? tips

The ⓘ button next to a container, stack or service opens a short reference card. Its accessible name is
**More information**. The card shows the category when it is known and an importance tag: **Essential**,
**Important**, **Optional** or **Unknown**. It then explains **What it is**, **If it fails** and, when
useful, **Origin**, for example the systemd unit or the stack and image behind a container.

Known services and images use the built-in catalogue; when Talaia does not know one, the card still
shows its origin or, for an unknown service, the description reported by systemd. Use the card before
stopping something: it tells you whether the server or your access depends on it and what to check
first.

**View full card** opens the detailed card in a side panel: what it is in plain words, **If you stop
it**, **If it fails** with its **Common problems** and how to fix them, **How to check it** with
commands you can copy (`systemctl status`, `journalctl` or `docker logs`, plus specific tools such as
`certbot certificates`), the origin and a link to the official documentation.

## Catalogue

**Catalogue**, under Administration, lists every service and container image Talaia recognises,
written for beginners. Search by name, id, unit or image, and filter by **Type**, **Category** and
**Importance**; the filters are kept in the address. **View card** opens the same detailed card as the
ⓘ button.

## Alerts

**Alerts** opens on **Active**, the inbox of current alerts. Other tabs are **History**, **Silences**,
and, for administrators, **Rules**, **Checks** and **Channels**. The inbox marks each alert as **New**,
**Seen** or **Silenced**. Search and the **Severity**, **State**, **Server** and **Rule** filters narrow
the list; **Group by** can group it by **Server** or **Rule**, and **Clear filters** resets it.

Select rows to **Acknowledge**, **Silence** or **Stop watching** in bulk. The bar explains which
selected alerts cannot be processed and why, and dangerous bulk actions ask for confirmation. Opening
a row shows the detail panel: **Where** the alert happened, **Actions** on its cause (for example
**Open server**, **Start container**, **Restart container**, **View logs**, **Open terminal**,
**Open stack**, **Open services** or **Clean up resources**), the **Timeline**, the
**Rule** with its threshold and an **Edit rule** link, and the **Metric** chart around the incident.
**Acknowledge** stops repeat notifications; **Silence** asks for a duration and a reason and suppresses
delivery without hiding the incident; **Stop watching** stops monitoring a container until it runs
again.

The alert bell in the top bar shows the number of open alerts and the latest new or critical ones.
**View all alerts** opens the inbox. Only accounts with `alerts.manage` see **Rules**, **Checks** and
**Channels**: **Rules** controls what raises an alert, **Checks** manages HTTP, TCP and TLS probes with
**Run now**, and **Channels** configures Telegram, ntfy, email, Discord and webhooks with **Send test**.

## Command palette

Press **Ctrl K** (or **⌘K** on a Mac), or use the **Search…** box in the top bar, to open the command
palette. It searches **Screens**, **Recent** items, **Servers**, **Containers**, **Stacks** and
**Actions**. The actions include **Add server**, **Switch theme**, **Change language** and **Sign out**.
Use the arrow keys to move, **Enter** to open the selected result and **Esc** to close the palette.

## Users, Audit and Settings

**Users** is available to administrators. **People**, **Teams** and **Groups** cover accounts, team
membership and server groups. From here you can invite a user, edit an account, enable or disable it,
delete it, and assign a role and scope. The role descriptions and access rules are in
[permissions.md](permissions.md).

**Audit** has the **Audit log** and **Recordings** tabs when your account has those permissions. The
log lists every recorded action newest first and can be filtered by action group, server and actor or
searched by text. **Details** opens the event drawer; terminal recordings can be downloaded from
**Recordings**.

**Settings** groups **Account**, **Servers**, **Integrations** and **System**. Account holds trusted
devices and passkeys; Servers shows agent versions, offers agent updates and removes a server after
typing its name; Integrations holds API keys and single sign-on; System checks the version, lists
pending commits and applies panel updates. Tabs appear only when your account can use them.

## Mobile

On a phone the sidebar becomes an overlay opened with the menu button. Breadcrumbs are hidden and the
search box becomes an icon that opens the same command palette. Tables turn into labelled cards, wide
tab bars scroll or wrap, and forms stack their fields. Row menus, bulk actions, confirmations, toasts
and the alert bell keep working. For a quick check, use **Overview** and **Alerts** first; the detail
panels and terminal are usable but need more scrolling.
