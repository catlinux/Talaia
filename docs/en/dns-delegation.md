# A trusted certificate with two DNS records

[Leer en castellano](../es/dns-delegation.md) · [Llegir en català](../ca/dns-delegation.md) · [Back to installation](installation.md#a-certificate-browsers-trust-acme-dns-01)

This is the recommended way to give the panel a certificate that browsers trust. You create two records in the
DNS zone of your domain, at whatever company hosts it, and the installer does the rest. No API key, no account
of your DNS provider on the server, nothing on ports 80 or 443: a web server such as Apache that already runs on
the machine is not touched.

## How it works

To issue a certificate for `talaia.example.com`, Let's Encrypt asks for a `TXT` record at
`_acme-challenge.talaia.example.com`. The `NS` record below delegates exactly that name, and nothing else, to
your server. Talaia runs a tiny DNS server there (the `acme-dns` service) that only answers that name, only while
a certificate is being issued or renewed.

## 1. Create the two records

In the DNS zone of `example.com` (replace with your domain and the name you chose):

| Type | Name (subdomain) | Value (target) |
|---|---|---|
| `A` | `talaia` | the public IPv4 address of your server |
| `NS` | `_acme-challenge.talaia` | `talaia.example.com` |

- Do **not** create an `AAAA` record for `talaia`: the challenge server answers on IPv4 only for now.
- The installer prints this table with your values, and the address of your server.

### At OVH

1. In the OVHcloud Control Panel, open **Domain names** and select your domain.
2. Open the **DNS zone** tab and click **Add an entry**.
3. For the `A` record: choose **A**, write `talaia` in **Subdomain** and the IP address of the server as target.
   Confirm with **Add**. If you already created it, check that the address is right.
4. For the `NS` record: **Add an entry** again, choose **NS**, write `_acme-challenge.talaia` in **Subdomain**
   and `talaia.example.com` in **Target (DNS server)**. If the form accepts it, end the target with a dot
   (`talaia.example.com.`); if not, leave it without. Confirm with **Add**.

### At other providers

The same two records exist at every DNS provider. Look for "add record" in the zone of your domain; the
`Name`/`Host` field takes the subdomain (`talaia`, `_acme-challenge.talaia`) and the `Value`/`Target` field the
address or the name. Some providers want the full name (`_acme-challenge.talaia.example.com`) instead.

Changes usually show up within minutes; OVH warns they can take longer to reach everybody.

## 2. Open port 53

The challenge server listens on UDP **and** TCP port 53 of the public address. The installer:

- asks before opening it in `ufw` or `firewalld` if one of them is active, and only does it if you say yes;
- reminds you to open it in the firewall of your provider (control panel, security group), which it cannot do.

The panel and agent ports (8443 and 8444 by default) need to be open in the same places.

## 3. Let the installer check

Once the records exist and the port is open, press Enter in the installer. It runs
`talaia-core acme check-delegation`, which prints one line per check:

| Line | Meaning and fix |
|---|---|
| `a-record: ok` | The name points to this server. |
| `a-record: missing` / `other` | Create or fix the `A` record (the line shows what it points to now). |
| `aaaa-record: present` | Remove the `AAAA` record of the name. |
| `delegation: ok` | The `NS` record exists in your zone and points to the panel name. |
| `delegation: missing` | The `NS` record is not there yet, or has another name. Create it as in step 1. |
| `delegation: other <name>` | The `NS` record points somewhere else. A target like `talaia.example.com.example.com.` means the provider added your domain to it: write the target with a final dot, or without the domain, as the form expects. |
| `dns-server: ok` | Your server answers on port 53 through the delegation. |
| `dns-server: unreachable` | Port 53 is closed somewhere: in `ufw`/`firewalld` or in the firewall of the provider. Open UDP and TCP 53. |
| `dns-server: not delegated yet` | The resolvers do not see the `NS` record yet. Wait a few minutes and check again. |

When everything is `ok`, the installer asks Let's Encrypt's test environment (staging) for a certificate. That
proves the whole chain from the internet without spending the limits of the real service. Then it asks for the
real one and says `ACME certificate: obtained`.

If something does not work you can continue without a trusted certificate (the panel uses Talaia's own) and run
`packaging/core/install.sh --acme delegation` later to finish.

## Security

- Nothing on the server can change your DNS zone: there are no provider keys. The `NS` record only lets this server
  answer `_acme-challenge.talaia.example.com`, which is only good for certificates of `talaia.example.com`.
- The DNS server runs in its own container, read only, without the database, the secrets or the data of the
  core; it only reads the pending challenge values, which are public anyway. It answers that name and refuses
  everything else, never resolves other names for anyone (no recursion), keeps its answers small and drops
  queries above a rate per source network, so it cannot be used to amplify attacks.
- Optional: a `CAA` record on `talaia` with the value `0 issue "letsencrypt.org"` tells every other certificate
  authority not to issue for that name, if your provider supports `CAA` records.
- More in [security.md](security.md).

## Undo it

In `packaging/core`:

```bash
sudo docker compose --profile acme-dns rm -sf acme-dns
```

then set `TALAIA_ACME_PROVIDER=` (empty) and remove the `COMPOSE_PROFILES=acme-dns` line in `.env`, run
`sudo docker compose up -d`, and delete the `NS` record. The panel goes back to the certificate of Talaia's own
CA.
