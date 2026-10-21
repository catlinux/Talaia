# Entrada con PIN y huella (dispositivo de confianza + passkeys)

Decisiones del mantenedor (2026-10-21): PIN de 6 a 12 dígitos **y** huella (passkey) a la vez; el segundo
factor (TOTP) se vuelve a pedir al abrir el navegador y cada 12 horas (ajustable). Contraseña + TOTP siguen
siendo el acceso base y la recuperación.

## Diseño
**Dispositivo de confianza (PIN)**
- Se crea desde Ajustes con sesión completa, reconfirmando contraseña: el servidor emite un secreto
  aleatorio de dispositivo (cookie `talaia_device`, HttpOnly, Secure, SameSite=Strict, guardada como hash) y el
  usuario elige el PIN (6–12 dígitos). El PIN se guarda con Argon2id (como la contraseña) ligado al
  dispositivo. El PIN solo se comprueba si llega con la cookie de un dispositivo válido.
- Entrada con PIN → sesión en fase `mfa_pending` si hace falta TOTP, o `full` si el dispositivo tiene un
  segundo factor reciente. Regla: el TOTP se exige cuando (a) la sesión de navegador es nueva (no hay cookie de
  sesión: se cerró el navegador) o (b) han pasado más de `device_mfa_hours` (defecto 12) desde el último TOTP
  correcto de ese dispositivo. En esos casos el PIN lleva a la pantalla de TOTP.
- 5 PIN erróneos seguidos borran el PIN de ese dispositivo (obliga a contraseña + TOTP); límite por IP como
  el login; auditoría (`device_trusted`, `login_pin`, `login_pin_failed`, `device_removed`).
- Máximo 10 dispositivos por usuario; se listan y se quitan en Ajustes; cambiar la contraseña los revoca todos.

**Passkeys (huella o PIN del dispositivo)**
- WebAuthn con `github.com/go-webauthn/webauthn`, `userVerification: required`. Una passkey vale como sesión
  completa (contraseña + segundo factor). RP ID = primer nombre público; origen con el puerto del panel.
- Alta solo con sesión completa y contraseña reconfirmada; máximo 10; nombrables y borrables. Contador de
  firmas comprobado; desafío de un solo uso con caducidad de 5 min. Auditoría `passkey_*`, `login_passkey`.
- Solo funciona por HTTPS con nombre de dominio; documentar que con CA propia puede ser rechazado.

## Pasos
1. **Núcleo PIN/dispositivos** (Sonnet alto + revisión de seguridad Opus): migración 0008, store, API
   `/api/v1/auth/devices` (crear, listar, borrar), `/api/v1/auth/pin/login`, ajuste `device_mfa_hours`.
2. **Núcleo passkeys**: migración 0009, store, API `/api/v1/auth/passkey/...` y `/api/v1/auth/passkeys`.
3. **Panel**: pantalla de acceso (PIN / huella / contraseña), gestión en Ajustes. ca/es/en.
4. **Documentación** EN+ES(+CA): acceso, seguridad, README, CHANGELOG. Versión: menor.
No tocar la lógica de TOTP ni la de recuperación.
