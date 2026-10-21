# Certificat de confiança amb dos registres DNS

[Read in English](../en/dns-delegation.md) · [Leer en castellano](../es/dns-delegation.md)

És la manera recomanada de donar al tauler un certificat en què confien els navegadors. Crees dos registres a la
zona DNS del teu domini, a l'empresa que la tingui, i l'instal·lador fa la resta. Sense claus d'API, sense cap
compte del teu proveïdor de DNS al servidor i sense fer servir els ports 80 ni 443: un servidor web com Apache que
ja funcioni a la màquina no es toca.

## Com funciona

Per emetre un certificat per a `talaia.example.com`, Let's Encrypt demana un registre `TXT` a
`_acme-challenge.talaia.example.com`. El registre `NS` de sota delega al teu servidor exactament aquest nom, i
res més. Talaia hi porta un servidor DNS mínim (el servei `acme-dns`) que només respon aquest nom i només mentre
s'emet o es renova un certificat.

## 1. Crea els dos registres

A la zona DNS d'`example.com` (substitueix-ho pel teu domini i el nom que hagis triat):

| Tipus | Nom (subdomini) | Valor (destinació) |
|---|---|---|
| `A` | `talaia` | l'adreça IPv4 pública del teu servidor |
| `NS` | `_acme-challenge.talaia` | `talaia.example.com` |

- **No** creïs un registre `AAAA` per a `talaia`: de moment el servidor del repte només respon per IPv4.
- L'instal·lador imprimeix aquesta taula amb els teus valors i l'adreça del teu servidor.

### A OVH

El tauler de control d'OVHcloud no té versió en català; les etiquetes són les de la versió anglesa.

1. Al tauler de control d'OVHcloud, obre **Domain names** i tria el teu domini.
2. Obre la pestanya **DNS zone** i prem **Add an entry**.
3. Per al registre `A`: tria **A**, escriu `talaia` a **Subdomain** i la IP del servidor com a destinació.
   Confirma amb **Add**. Si ja el vas crear, comprova que l'adreça és la correcta.
4. Per al registre `NS`: un altre cop **Add an entry**, tria **NS**, escriu `_acme-challenge.talaia` a
   **Subdomain** i `talaia.example.com` a **Target (DNS server)**. Si el formulari ho accepta, acaba la
   destinació amb un punt (`talaia.example.com.`); si no, deixa-la sense. Confirma amb **Add**.

### En altres proveïdors

Els mateixos dos registres existeixen a qualsevol proveïdor de DNS. Busca «afegir registre» a la zona del teu
domini; el camp `Nom`/`Host` porta el subdomini (`talaia`, `_acme-challenge.talaia`) i el camp
`Valor`/`Destinació`, l'adreça o el nom. Alguns proveïdors demanen el nom complet
(`_acme-challenge.talaia.example.com`).

Els canvis se solen veure en pocs minuts; OVH avisa que poden trigar més a arribar a tothom.

## 2. Obre el port 53

El servidor del repte escolta al port 53 UDP **i** TCP de l'adreça pública. L'instal·lador:

- pregunta abans d'obrir-lo a `ufw` o `firewalld` si algun és actiu, i només ho fa si dius que sí;
- et recorda obrir-lo al tallafoc del teu proveïdor (tauler de control, grup de seguretat), que ell no pot tocar.

Els ports del tauler i dels agents (8443 i 8444 per defecte) han d'estar oberts als mateixos llocs.

## 3. Deixa que l'instal·lador ho comprovi

Quan els registres existeixin i el port estigui obert, prem Enter a l'instal·lador. Executa
`talaia-core acme check-delegation`, que imprimeix una línia per comprovació:

| Línia | Què vol dir i com arreglar-ho |
|---|---|
| `a-record: ok` | El nom apunta a aquest servidor. |
| `a-record: missing` / `other` | Crea o corregeix el registre `A` (la línia diu on apunta ara). |
| `aaaa-record: present` | Esborra el registre `AAAA` del nom. |
| `delegation: ok` | El registre `NS` existeix a la teva zona i apunta al nom del tauler. |
| `delegation: missing` | El registre `NS` encara no hi és, o té un altre nom. Crea'l com al pas 1. |
| `delegation: other <nom>` | El registre `NS` apunta a un altre lloc. Una destinació com `talaia.example.com.example.com.` vol dir que el proveïdor hi ha afegit el teu domini: escriu la destinació amb punt final, o sense el domini, segons demani el formulari. |
| `dns-server: ok` | El teu servidor respon al port 53 a través de la delegació. |
| `dns-server: unreachable` | El port 53 està tancat en algun lloc: a `ufw`/`firewalld` o al tallafoc del proveïdor. Obre el 53 UDP i TCP. |
| `dns-server: not delegated yet` | Els resolvedors encara no veuen el registre `NS`. Espera uns minuts i torna a comprovar. |

Quan tot està en `ok`, l'instal·lador demana un certificat a l'entorn de proves de Let's Encrypt (*staging*). Això
demostra tota la cadena des d'internet sense gastar els límits del servei real. Després demana el de veritat i
diu `ACME certificate: obtained`.

Si alguna cosa no funciona, pots continuar sense certificat de confiança (el tauler fa servir el de Talaia) i
executar `packaging/core/install.sh --acme delegation` més endavant per acabar.

## Seguretat

- Res del servidor pot canviar la teva zona DNS: no hi ha claus del proveïdor. El registre `NS` només permet a
  aquest servidor respondre `_acme-challenge.talaia.example.com`, que només serveix per a certificats de
  `talaia.example.com`.
- El servidor DNS corre al seu propi contenidor, de només lectura, sense la base de dades, els secrets ni les dades
  del nucli; només llegeix els valors pendents del repte, que de tota manera són públics. Respon aquest nom i
  rebutja tota la resta, mai resol altres noms per a ningú (sense recursió), manté les respostes petites i descarta
  les consultes que passen d'un ritme per xarxa d'origen, així que no serveix per amplificar atacs.
- Opcional: un registre `CAA` a `talaia` amb el valor `0 issue "letsencrypt.org"` indica a qualsevol altra autoritat
  de certificació que no emeti per a aquest nom, si el teu proveïdor admet registres `CAA`.
- Més a [security.md](../en/security.md) (en anglès) o [seguridad.md](../es/seguridad.md) (en castellà).

## Desfer-ho

A `packaging/core`:

```bash
sudo docker compose --profile acme-dns rm -sf acme-dns
```

després posa `TALAIA_ACME_PROVIDER=` (buit) i treu la línia `COMPOSE_PROFILES=acme-dns` del `.env`, executa
`sudo docker compose up -d` i esborra el registre `NS`. El tauler torna al certificat de la CA pròpia de Talaia.
