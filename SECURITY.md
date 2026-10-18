# Security policy

[Leer en castellano](SECURITY.es.md)

Talaia manages servers with root privileges. Security problems are taken seriously.

## Supported versions

Talaia has not published a stable release yet. Until 1.0.0, only the latest version on `main` receives
security fixes.

| Version | Supported |
|---|---|
| latest `main` | ✅ |
| older | ❌ |

## Reporting a vulnerability

**Do not open a public issue.**

Report it privately through GitHub: go to the repository's **Security** tab and choose
**Report a vulnerability**
(<https://github.com/catlinux/Talaia/security/advisories/new>).

Please include:

- affected component (`core`, `agent`, `web`, `cli`, packaging…) and version or commit;
- description of the problem and its impact;
- steps to reproduce or a proof of concept;
- any suggested fix.

## What to expect

- Acknowledgement within 7 days.
- An assessment and a plan within 30 days.
- Credit in the release notes if you want it, once the fix is published.

## Scope

In scope: everything in this repository. Out of scope: vulnerabilities in third-party software
(Docker, Debian packages, Caddy…) unless Talaia uses it in an insecure way.
