# Sign-in and access

[Leer en castellano](../es/acceso.md) · [Llegir en català](../ca/access.md) · [Back to README](../../README.md)

Talaia always starts from a password and a TOTP code (the six digits of an authenticator app), with the
recovery codes as the way back in. On top of that base you can add two faster methods:

- a **PIN on a trusted device**, for a browser you use regularly;
- **passkeys**, which use the fingerprint, the face or the device PIN through WebAuthn.

Both are optional. Password + TOTP and the recovery codes keep working, and they remain the recovery path if a
phone, a browser or an authenticator is lost. The design behind them is in [security.md](security.md); the
installation steps are in [installation.md](installation.md).

## PIN on a trusted device

A trusted device is one browser that may open a full session with a PIN. It is not a second administrator
account and it does not replace the password everywhere: another browser still signs in with the password and
TOTP.

### How it works

When you trust a browser, the core sends it a long-lived `__Host-talaia_device` cookie (180 days, `HttpOnly`,
`Secure`, `SameSite=Strict`) and stores only its hash. The browser also keeps the non-secret device name in
`localStorage` so it can show the PIN form. The PIN is stored hashed, never in clear.

The PIN is useless without that cookie. Copying the PIN to another browser does not work, and deleting the
cookie makes the browser ask for the password again.

The second factor is not removed: it is remembered for a while. TOTP is asked again when the browser has been
closed (the `__Host-talaia_browser` marker cookie is a session cookie) or when more than `device_mfa_hours` has
passed since the last TOTP on that device. The default is 12 hours; the range is 1 to 168 hours. Set it with
`TALAIA_DEVICE_MFA_HOURS` in the environment or `device_mfa_hours` in the TOML configuration.

"Closed" means that the browser discarded its session cookies. Phone browsers do it when the app is closed; Chrome
on a computer keeps them if **Continue where you left off** is on or if it keeps **running in the background
after you close it** (Settings > System), so there only the time limit applies. If you want the second factor
at every browser start, turn both settings off, or lower `device_mfa_hours`.

Five wrong PINs disable the device: its PIN is cleared and it has to be added again. A user can have at most
10 active trusted devices.

### Set up

1. Sign in with the password and TOTP.
2. Open **Settings > Access**.
3. Under **Trusted devices**, fill in **Device name** (for example, the name of the laptop), **Current
   password**, **PIN** and **Confirm PIN**.
4. Select **Trust this device**.

The PIN must have 6 to 12 digits. It cannot use the same digit repeatedly, and it cannot be a straight
ascending or descending run (`123456` or `654321`, for example). The current password is checked again when
the device is created.

### Use it

The next time you open the panel in that browser, the PIN form is the primary form. Type the PIN and select
**Enter with PIN**:

- if the browser marker and the 12-hour window are still valid, the session opens directly;
- otherwise the panel asks for the TOTP code and then continues normally.

**Use password** is always available. It opens the usual password form without removing the device.

### Remove it

Open **Settings > Access > Trusted devices** and select **Remove** next to the device. If it is the current
browser, the panel also forgets the local device name; the next sign-in starts from the password.

Clearing the browser's cookies or site data also removes the device cookie from that browser, but it does not
delete the device from the account: remove it in Settings as well. Changing the password, or resetting the TOTP
secret, removes all trusted devices automatically.

### Security notes

- The device cookie is a bearer credential for the PIN form. Use a screen lock on the browser device and remove
  a trusted device as soon as that device is lost, sold or shared.
- The PIN protects the local step, not the account on its own. The password and TOTP are still required on a
  new browser.
- A wrong PIN does not reveal whether the device or the PIN was wrong, and five failures disable the device.
- At most 10 devices can be active per user. When the limit is reached, remove one first.

## Passkeys

A passkey is a WebAuthn credential stored by the browser or the operating system: the fingerprint, the face or
the device PIN unlocks it. Talaia requires user verification, so the authenticator must confirm the person
before it signs the challenge.

### How it works

A passkey counts as the password and the second factor at the same time. After a successful passkey sign-in,
Talaia opens a full session and does not ask for TOTP. Password + TOTP and recovery codes remain available for
the cases where the passkey cannot be used.

A user can have at most 10 passkeys. Each registration challenge and each login challenge expires after five
minutes. If the authenticator's signature counter goes backwards, Talaia treats it as a possible cloned
authenticator and disables that passkey; add it again only after checking the device.

### Requirements

Passkeys need HTTPS with a DNS name in `public_hostnames` (`TALAIA_PUBLIC_HOSTNAMES` in `.env`, or
`public_hostnames` in the TOML configuration). An IP address cannot be a WebAuthn relying-party ID, so a
deployment configured only with IP addresses does not offer passkeys.

With Talaia's own CA, some browsers refuse to create or use passkeys. Trust the CA on the device, or install the
panel with a certificate the browser trusts.

### Set up

1. Sign in with the password and TOTP.
2. Open **Settings > Access**.
3. Under **Passkeys**, select **Add a passkey**.
4. Type a **Passkey name** and your **Current password**.
5. Confirm the browser prompt with the fingerprint, the face or the device PIN.

If the deployment does not support passkeys, the card says **Passkeys need HTTPS with a domain name** and
does not show the form.

### Use it

On the sign-in screen, select **Sign in with fingerprint or PIN of this device**. Choose the passkey if the
browser asks, and confirm with the authenticator. Cancelling the prompt leaves you on the sign-in screen with
no error; the password form is still there.

### Remove it

Open **Settings > Access > Passkeys** and select **Remove** next to the passkey. The private key stays on the
authenticator; removing the passkey only deletes Talaia's public-key record, so the same authenticator can be
registered again later. Remove a passkey you no longer control before adding its replacement.

If you lose every passkey, sign in with the password, TOTP and, if needed, a recovery code, then add a new
passkey from Settings.

### Security notes

- Talaia never receives the private key; it stores the public key and checks the signature against the panel's
  domain.
- A passkey is bound to the site it was created for, which makes it resistant to password phishing. The device
  PIN or biometric check is still required by Talaia (user verification).
- Keep at least one other way in: the password + TOTP, and the recovery codes in a safe place.
- A passkey does not bypass account lock-out or the audit log: passkey sign-ins are recorded like password
  sign-ins.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| The PIN form does not appear | This browser has no remembered device, the device cookie was deleted or expired, the password was changed, or the device was removed. Sign in with the password and trust the browser again. |
| The panel asks for TOTP even though the PIN works | The browser was closed or the `device_mfa_hours` window expired. This is expected; enter the TOTP code and the PIN form returns on the next sign-in. |
| The PIN is rejected and the form disappears | Five wrong PINs disable the device. Sign in with the password and TOTP, then add the device again with a new PIN. |
| The passkey button does not appear | The browser lacks WebAuthn, the deployment has no HTTPS DNS name in `public_hostnames`, or the passkey was removed. Use password + TOTP; check the passkey requirements above. |
| The passkey prompt fails or the browser does not trust the panel | With the own CA, some browsers refuse passkeys. Trust the CA on the device or use a certificate the browser trusts, then try again. |
| A passkey stopped working | The authenticator may have reported a signature counter below the stored one, so Talaia disabled it. Check the device and add the passkey again. |
| The device or the authenticator is lost | Use the password, TOTP and recovery codes. Remove the lost device or passkey in **Settings > Access** once you are back in. |

## See also

- [Security](security.md): what protects the panel and what is recorded.
- [Installation](installation.md): first sign-in, ports, agents and updates.
