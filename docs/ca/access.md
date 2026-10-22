# Accés i inici de sessió

[Read in English](../en/access.md) · [Leer en castellano](../es/acceso.md) · [Tornar al README](../../README.md)

Talaia sempre parteix d'una contrasenya i un codi TOTP (les sis xifres d'una aplicació d'autenticació), amb
els codis de recuperació com a via de tornada. A partir d'aquesta base pots afegir dos mètodes més ràpids:

- un **PIN en un dispositiu de confiança**, per a un navegador que facis servir habitualment;
- **claus d'accés** (passkeys), que fan servir l'empremta, la cara o el PIN del dispositiu mitjançant WebAuthn.

Tots dos són opcionals. La contrasenya + TOTP i els codis de recuperació continuen funcionant, i continuen sent
la via de recuperació si es perd un telèfon, un navegador o una aplicació d'autenticació. El disseny és a
[../en/security.md](../en/security.md) (en anglès) i a [../es/seguridad.md](../es/seguridad.md) (en castellà);
els passos d'instal·lació, a [../en/installation.md](../en/installation.md) i
[../es/instalacion.md](../es/instalacion.md).

## PIN en un dispositiu de confiança

Un dispositiu de confiança és un navegador que pot obrir una sessió completa amb un PIN. No és un segon compte
d'administrador ni substitueix la contrasenya a tot arreu: un altre navegador continua entrant amb la
contrasenya i TOTP.

### Com funciona

Quan confies en un navegador, el nucli li envia una galeta de llarga durada `__Host-talaia_device` (180 dies,
`HttpOnly`, `Secure`, `SameSite=Strict`) i només en guarda el hash. El navegador també conserva el nom del
dispositiu, que no és secret, a `localStorage` per poder mostrar el formulari del PIN. El PIN es guarda amb
hash, mai en clar.

El PIN no serveix sense aquesta galeta. Copiar el PIN a un altre navegador no funciona, i esborrar la galeta fa
que el navegador torni a demanar la contrasenya.

El segon factor no desapareix: es recorda durant un temps. Es torna a demanar el TOTP quan s'ha tancat el
navegador (la galeta marcadora `__Host-talaia_browser` és de sessió) o quan han passat més de `device_mfa_hours`
des del darrer TOTP d'aquell dispositiu. El valor per defecte és de 12 hores; el rang va d'1 a 168 hores. Es
configura amb `TALAIA_DEVICE_MFA_HOURS` a l'entorn o `device_mfa_hours` al fitxer TOML.

«Tancat» vol dir que el navegador ha descartat les galetes de sessió. Els navegadors del mòbil ho fan en tancar
l'aplicació; Chrome a l'ordinador les conserva si està activat **Continua des d'on ho has deixat** o si
continua **executant-se en segon pla en tancar-lo** (Configuració > Sistema), i llavors només compta el límit
de temps. Si vols el segon factor a cada inici del navegador, desactiva tots dos ajustes o abaixa
`device_mfa_hours`.

Cinc PIN errònis desactiven el dispositiu: s'esborra el PIN i cal tornar-lo a afegir. Un usuari pot tenir com a
màxim 10 dispositius de confiança actius.

### Configurar-lo

1. Entra amb la contrasenya i TOTP.
2. Obre **Configuració > Accés**.
3. A **Dispositius de confiança**, omple **Nom del dispositiu** (per exemple, el nom del portàtil),
   **Contrasenya actual**, **PIN** i **Confirma el PIN**.
4. Prem **Confia en aquest dispositiu**.

El PIN ha de tenir de 6 a 12 xifres. No pot fer servir la mateixa xifra repetida ni ser una seqüència seguida
ascendent o descendent (`123456` o `654321`, per exemple). La contrasenya actual es comprova una altra vegada
en crear el dispositiu.

### Fer-lo servir

La propera vegada que obris el panell en aquell navegador, el formulari del PIN serà el principal. Escriu el PIN
i prem **Entra amb PIN**:

- si la galeta marcadora i la finestra de 12 hores continuen vigents, la sessió s'obre directament;
- si no, el panell demana el codi TOTP i després continua amb normalitat.

**Usar la contrasenya** sempre hi és. Obre el formulari habitual sense eliminar el dispositiu.

### Treure'l

Obre **Configuració > Accés > Dispositius de confiança** i prem **Elimina** al costat del dispositiu. Si és el
navegador actual, el panell també oblida el nom local del dispositiu; el proper accés comença per la
contrasenya.

Esborrar les galetes o les dades del lloc al navegador també elimina la galeta del dispositiu en aquell
navegador, però no esborra el dispositiu del compte: elimina'l també a Configuració. Canviar la contrasenya, o
restablir el secret TOTP, elimina tots els dispositius de confiança automàticament.

### Notes de seguretat

- La galeta del dispositiu és una credencial d'accés per al formulari del PIN. Fes servir el bloqueig de pantalla
  al dispositiu del navegador i elimina un dispositiu de confiança tan bon punt es perdi, es vengui o es
  comparteixi.
- El PIN protegeix el pas local, no el compte tot sol. En un navegador nou encara calen la contrasenya i el
  TOTP.
- Un PIN erròni no revela si ha fallat el dispositiu o el PIN, i cinc errors desactiven el dispositiu.
- Com a màxim hi pot haver 10 dispositius actius per usuari. Si s'arriba al límit, elimina'n un abans.

## Claus d'accés

Una clau d'accés és una credencial WebAuthn guardada pel navegador o el sistema operatiu: l'empremta, la cara o
el PIN del dispositiu la desbloquegen. Talaia exigeix la verificació de l'usuari, així que l'autenticador ha de
confirmar la persona abans de signar el repte.

### Com funciona

Una clau d'accés compta com la contrasenya i el segon factor alhora. Després d'un accés correcte amb clau,
Talaia obre una sessió completa i no demana TOTP. La contrasenya + TOTP i els codis de recuperació continuen
disponibles per als casos en què no es pugui fer servir la clau.

Un usuari pot tenir com a màxim 10 claus d'accés. Cada repte de registre i cada repte d'accés caduca als cinc
minuts. Si el comptador de signatures de l'autenticador retrocedeix, Talaia ho interpreta com un possible
autenticador clonat i desactiva aquella clau; torna-la a afegir només després de revisar el dispositiu.

### Requisits

Les claus d'accés necessiten HTTPS amb un nom DNS a `public_hostnames` (`TALAIA_PUBLIC_HOSTNAMES` a `.env`, o
`public_hostnames` al fitxer TOML). Una adreça IP no pot ser l'identificador del lloc de confiança
(relying-party ID) de WebAuthn, així que un desplegament configurat només amb IP no ofereix claus d'accés.

Amb la CA pròpia de Talaia, alguns navegadors es neguen a crear o fer servir claus d'accés. Confia en la CA al
dispositiu o instal·la el panell amb un certificat en què confiï el navegador.

### Configurar-les

1. Entra amb la contrasenya i TOTP.
2. Obre **Configuració > Accés**.
3. A **Claus d'accés**, prem **Afegeix una clau d'accés**.
4. Escriu un **Nom de la clau d'accés** i la teva **Contrasenya actual**.
5. Confirma l'avís del navegador amb l'empremta, la cara o el PIN del dispositiu.

Si el desplegament no admet claus d'accés, la targeta mostra **Les claus d'accés necessiten HTTPS amb un nom de
domini** i no ensenya el formulari.

### Fer-les servir

A la pantalla d'accés, prem **Entra amb l'empremta o el PIN d'aquest dispositiu**. Tria la clau si el navegador
ho pregunta i confirma amb l'autenticador. Cancel·lar l'avís et deixa a la pantalla d'accés sense error; el
formulari de contrasenya continua allà.

### Treure-les

Obre **Configuració > Accés > Claus d'accés** i prem **Elimina** al costat de la clau. La clau privada es queda a
l'autenticador; eliminar la clau només esborra el registre de clau pública de Talaia, així que el mateix
autenticador es pot tornar a registrar més endavant. Elimina una clau que ja no controlis abans d'afegir-ne una
de nova.

Si perds totes les claus d'accés, entra amb la contrasenya, el TOTP i, si cal, un codi de recuperació; després
afegeix una clau nova des de Configuració.

### Notes de seguretat

- Talaia no rep mai la clau privada; guarda la clau pública i comprova la signatura contra el domini del panell.
- Una clau d'accés està lligada al lloc per al qual es va crear, cosa que la fa resistent al phishing de
  contrasenyes. Talaia continua exigint el PIN o la comprovació biomètrica del dispositiu (verificació de
  l'usuari).
- Conserva almenys una altra manera d'entrar: la contrasenya + TOTP i els codis de recuperació en un lloc segur.
- Una clau d'accés no es salta el bloqueig del compte ni el registre d'auditoria: els accessos amb clau queden
  registrats com els accessos amb contrasenya.

## Problemes freqüents

| Símptoma | Causa i solució |
|---|---|
| No apareix el formulari del PIN | Aquest navegador no té cap dispositiu recordat, s'ha esborrat o ha caducat la galeta del dispositiu, s'ha canviat la contrasenya o s'ha eliminat el dispositiu. Entra amb la contrasenya i torna a confiar en el navegador. |
| El panell demana TOTP encara que el PIN funcioni | S'ha tancat el navegador o ha caducat la finestra de `device_mfa_hours`. És l'esperat; escriu el codi TOTP i el formulari del PIN tornarà al proper accés. |
| El PIN es rebutja i el formulari desapareix | Cinc PIN errònis desactiven el dispositiu. Entra amb la contrasenya i TOTP i torna a afegir-lo amb un PIN nou. |
| No apareix el botó de la clau d'accés | El navegador no té WebAuthn, el desplegament no té cap nom DNS HTTPS a `public_hostnames` o s'ha eliminat la clau. Fes servir contrasenya + TOTP i revisa els requisits de dalt. |
| L'avís de la clau falla o el navegador no confia en el panell | Amb la CA pròpia, alguns navegadors rebutgen les claus. Confia en la CA al dispositiu o fes servir un certificat en què confiï el navegador, i torna-ho a provar. |
| Una clau d'accés ha deixat de funcionar | Potser l'autenticador ha informat d'un comptador de signatures més baix que el guardat i Talaia l'ha desactivat. Revisa el dispositiu i torna a afegir la clau. |
| Es perd el dispositiu o l'autenticador | Fes servir la contrasenya, el TOTP i els codis de recuperació. Quan tornis a entrar, elimina el dispositiu o la clau perduts a **Configuració > Accés**. |

## Vegeu també

- [Seguretat (en castellà)](../es/seguridad.md) i [Security (en anglès)](../en/security.md): què protegeix el
  panell i què es grava.
- [Instal·lació (en castellà)](../es/instalacion.md) i [Installation (en anglès)](../en/installation.md): primer
  accés, ports, agents i actualitzacions.
