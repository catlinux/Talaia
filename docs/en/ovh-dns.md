# A trusted certificate with OVH: get the API key

[Leer en castellano](../es/ovh-dns.md) · [Llegir en català](../ca/ovh-dns.md) · [Back to installation](installation.md#a-certificate-browsers-trust-acme-dns-01)

Without this, the panel works but the browser warns that the connection is "not secure", because Talaia signs
its certificate with its own authority. If the DNS of your domain is at OVH, Talaia can get a free certificate
from Let's Encrypt by itself. For that it needs an OVH key allowed to change **only your DNS zone**. This guide
creates it, step by step. It takes about five minutes.

**How it works.** Let's Encrypt asks Talaia to prove that the domain is yours by publishing a temporary `TXT`
record in your DNS zone. Talaia does it through the OVH API with this key, and removes the record afterwards.
That is why nothing has to listen on ports 80 or 443, and why a server running Apache or another web server is
not affected. The key stays on your server, in `packaging/core/.env.acme` (readable only by root).

## Before you start

- The domain must use the **OVH name servers** (Web Cloud → Domain names → your domain → *DNS servers*). If it
  uses other name servers, create the key at the company that hosts your DNS instead; Talaia supports OVH only for
  now, and without it you can still use the panel with its own certificate.
- Create, in the DNS zone, the record that points the name you chose to your server (Web Cloud → Domain names →
  your domain → *DNS zone* → *Add an entry*): type **A** (and **AAAA** if the server has an IPv6 address), the
  subdomain (for example `talaia`) and the public IP address of your server. The installer warns if the name does
  not resolve yet. You do **not** need to configure anything in Apache or in the server.
- Know your **zone**: it is the registered domain, not the subdomain. For `talaia.example.com` the zone is
  `example.com`.

## 1. Open the key creation page

The installer prints a link already prepared for your zone. If you prefer to build it yourself, replace
`example.com` with your zone:

- OVH Europe (accounts of ovh.com in Europe and most of the world):

  ```text
  https://api.eu.ovhcloud.com/createToken/?POST=/domain/zone/example.com/record&POST=/domain/zone/example.com/refresh&DELETE=/domain/zone/example.com/record/*
  ```

- OVH Canada:

  ```text
  https://ca.api.ovh.com/createToken/?POST=/domain/zone/example.com/record&POST=/domain/zone/example.com/refresh&DELETE=/domain/zone/example.com/record/*
  ```

Sign in with your OVH account when it asks.

## 2. Fill in the form

| Field | What to put |
|---|---|
| Application name and description | Anything you will recognise later, for example `Talaia` and `Certificate of the Talaia panel`. |
| Validity | The longest one, **unlimited** if it is offered. Talaia renews the certificate every 60 days; if the key expires, the renewal fails and the panel goes back to the warning. |
| Rights | These three, and nothing else. They should appear already filled in from the link; if the list is empty or incomplete, add the missing ones with the **+** button. |

The three rights, with your zone instead of `example.com`:

| Method | Path |
|---|---|
| `POST` | `/domain/zone/example.com/record` |
| `POST` | `/domain/zone/example.com/refresh` |
| `DELETE` | `/domain/zone/example.com/record/*` |

They allow adding and removing records in that zone, and nothing in your account or in other zones; they are the
only calls Talaia makes. Do **not** use `*` for every method and path: whoever got hold of that key would
control your whole OVH account.

Create the keys.

## 3. Copy the three values

OVH shows three values. **Copy them now**, they cannot be shown again:

| OVH calls it | Looks like | Notes |
|---|---|---|
| Application key | 16 characters | The installer asks for it as `OVH application key`. |
| Application secret | 32 characters | Secret. Asked without echo. |
| Consumer key | 32 characters | Secret. Asked without echo. |

Also note the **endpoint**: `ovh-eu` if you used the Europe link, `ovh-ca` for Canada.

## 4. Give them to the installer

Run the installer with `--acme ovh` (or answer yes when it asks) and paste the three values when it asks. It
checks them with OVH **before** building anything, and tells you exactly what is wrong:

- the key does not exist or has expired;
- it has not got the three rights (and which one is missing);
- OVH cannot be reached.

If you prefer not to type them, export them first:

```bash
export OVH_ENDPOINT=ovh-eu OVH_APPLICATION_KEY=… OVH_APPLICATION_SECRET=… OVH_CONSUMER_KEY=…
packaging/core/install.sh --hostname talaia.example.com --acme ovh
```

Within a few minutes of starting, the installer reports `ACME certificate: obtained`.

## If something fails

| What you see | Likely cause and fix |
|---|---|
| The installer says the key is not valid | A value was mistyped, or the key has expired. Create a new one (step 2) and run the installer again. |
| It lists a missing right | Create a new key with the three rights above (step 2) and run the installer again. |
| `ACME certificate: still pending` | Wait a few minutes (DNS can take time) and look at `sudo docker compose logs core \| grep -i acme`. The core keeps trying and the panel works meanwhile with its own certificate. |
| The log says the zone is not found or the name is not in OVH | The domain does not use OVH name servers, or the zone is wrong. Check *Before you start*. |
| The browser still warns after the certificate was obtained | You opened the panel by IP address, or by a name that is not in `--hostname`. Use the name. |

## Change or revoke the key

- **Change it:** create a new one, edit `packaging/core/.env.acme` (four lines, mode 0600) and run
  `sudo docker compose up -d` in `packaging/core`.
- **Revoke it** if it leaks or you stop using Talaia: in your OVH account, in the API keys of the control panel,
  or with the API (`/me/api/application`). The certificate keeps working until it expires, but cannot be renewed.
