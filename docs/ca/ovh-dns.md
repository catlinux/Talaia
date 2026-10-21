# Certificat de confiança amb OVH: obtenir la clau de l'API

[Read in English](../en/ovh-dns.md) · [Leer en castellano](../es/ovh-dns.md)

Sense això, el tauler funciona, però el navegador avisa que la connexió «no és segura», perquè Talaia signa el seu
certificat amb la seva pròpia autoritat. Si el DNS del teu domini és a OVH, Talaia pot aconseguir sol un certificat
gratuït de Let's Encrypt. Per a això necessita una clau d'OVH autoritzada a canviar **només la teva zona DNS**.
Aquesta guia la crea pas a pas i porta uns cinc minuts.

**Com funciona.** Let's Encrypt demana a Talaia que demostri que el domini és teu publicant un registre `TXT`
temporal a la teva zona DNS. Talaia ho fa per l'API d'OVH amb aquesta clau i l'esborra després. Per això no cal que
res escolti als ports 80 o 443, i un servidor amb Apache o un altre servidor web no es veu afectat. La clau es
queda al teu servidor, a `packaging/core/.env.acme` (només llegible per root).

## Abans de començar

- El domini ha d'usar els **servidors de noms d'OVH** (Web Cloud → Noms de domini → el teu domini →
  *Servidors DNS*). Si en fa servir uns altres, crea la clau a l'empresa que allotja el teu DNS; de moment Talaia
  només admet OVH, i sense ella pots usar el tauler amb el seu certificat propi.
- Crea, a la zona DNS, el registre que apunta el nom triat al teu servidor (Web Cloud → Noms de domini → el teu
  domini → *Zona DNS* → *Afegir una entrada*): tipus **A** (i **AAAA** si el servidor té IPv6), el subdomini (per
  exemple `talaia`) i l'adreça IP pública del teu servidor. L'instal·lador avisa si el nom encara no resol. **No**
  cal configurar res a Apache ni al servidor.
- Esbrina la teva **zona**: és el domini registrat, no el subdomini. Per a `talaia.example.com` la zona és
  `example.com`.

## 1. Obre la pàgina de creació de claus

L'instal·lador imprimeix un enllaç ja preparat per a la teva zona. Si prefereixes construir-lo tu, substitueix
`example.com` per la teva zona:

- OVH Europa (comptes d'ovh.com a Europa i la major part del món):

  ```text
  https://api.eu.ovhcloud.com/createToken/?POST=/domain/zone/example.com/record&POST=/domain/zone/example.com/refresh&DELETE=/domain/zone/example.com/record/*
  ```

- OVH Canadà:

  ```text
  https://ca.api.ovh.com/createToken/?POST=/domain/zone/example.com/record&POST=/domain/zone/example.com/refresh&DELETE=/domain/zone/example.com/record/*
  ```

Inicia sessió amb el teu compte d'OVH quan ho demani.

## 2. Omple el formulari

| Camp | Què posar |
|---|---|
| Nom i descripció de l'aplicació | Allò que reconeguis més endavant, per exemple `Talaia` i `Certificat del tauler de Talaia`. |
| Validesa | La més llarga, **il·limitada** si s'ofereix. Talaia renova el certificat cada 60 dies; si la clau caduca, la renovació falla i el tauler torna a l'avís. |
| Drets | Aquests tres i res més. Haurien d'aparèixer ja emplenats des de l'enllaç; si la llista és buida o incompleta, afegeix els que falten amb el botó **+**. |

Els tres drets, amb la teva zona en lloc d'`example.com`:

| Mètode | Ruta |
|---|---|
| `POST` | `/domain/zone/example.com/record` |
| `POST` | `/domain/zone/example.com/refresh` |
| `DELETE` | `/domain/zone/example.com/record/*` |

Permeten afegir i esborrar registres en aquesta zona, i res del teu compte ni d'altres zones; són les úniques
crides que fa Talaia. **No** facis servir `*` per a tots els mètodes i rutes: qui s'apoderés d'aquesta clau
controlaria tot el teu compte d'OVH.

Crea les claus.

## 3. Copia els tres valors

OVH mostra tres valors. **Copia'ls ara**, no es poden tornar a mostrar:

| Com li diu OVH | Aspecte | Notes |
|---|---|---|
| Application key | 16 caràcters | L'instal·lador la demana com a `OVH application key`. |
| Application secret | 32 caràcters | Secret. Es demana sense eco. |
| Consumer key | 32 caràcters | Secret. Es demana sense eco. |

Anota també l'**endpoint**: `ovh-eu` si has fet servir l'enllaç d'Europa, `ovh-ca` per a Canadà.

## 4. Dona-los a l'instal·lador

Executa l'instal·lador amb `--acme ovh` (o respon que sí quan ho pregunti) i enganxa els tres valors quan els
demani. Els comprova amb OVH **abans** de construir res i et diu exactament què falla:

- la clau no existeix o ha caducat;
- no té els tres drets (i quin falta);
- no es pot arribar a OVH.

Si prefereixes no teclejar-los, exporta'ls abans:

```bash
export OVH_ENDPOINT=ovh-eu OVH_APPLICATION_KEY=… OVH_APPLICATION_SECRET=… OVH_CONSUMER_KEY=…
packaging/core/install.sh --hostname talaia.example.com --acme ovh
```

Als pocs minuts d'arrencar, l'instal·lador informa d'`ACME certificate: obtained`.

## Si alguna cosa falla

| Què veus | Causa probable i solució |
|---|---|
| L'instal·lador diu que la clau no és vàlida | Un valor està mal escrit o la clau ha caducat. Crea'n una de nova (pas 2) i torna a executar l'instal·lador. |
| Llista un dret que falta | Crea una clau nova amb els tres drets de dalt (pas 2) i torna a executar l'instal·lador. |
| `ACME certificate: still pending` | Espera uns minuts (el DNS pot trigar) i mira `sudo docker compose logs core \| grep -i acme`. El nucli continua intentant-ho i, mentrestant, el tauler funciona amb el seu certificat propi. |
| El registre diu que no troba la zona o que el nom no és a OVH | El domini no usa els servidors de noms d'OVH, o la zona és incorrecta. Revisa *Abans de començar*. |
| El navegador continua avisant després d'obtenir el certificat | Has obert el tauler per adreça IP o per un nom que no és a `--hostname`. Fes servir el nom. |

## Canviar o revocar la clau

- **Canviar-la:** crea'n una de nova, edita `packaging/core/.env.acme` (quatre línies, mode 0600) i executa
  `sudo docker compose up -d` a `packaging/core`.
- **Revocar-la** si es filtra o deixes de fer servir Talaia: al teu compte d'OVH, a les claus d'API del tauler de
  control, o amb l'API (`/me/api/application`). El certificat continua funcionant fins que caduca, però no es pot
  renovar.
