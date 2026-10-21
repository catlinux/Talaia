# Entrada con PIN y huella (passkeys / WebAuthn)

Objetivo: entrar al panel con la huella o el PIN del dispositivo (passkey con verificación de usuario) en vez
de contraseña + TOTP. La contraseña y el TOTP siguen existiendo (respaldo y recuperación).

## Decisiones (a confirmar por el mantenedor antes del paso 1)
- Una passkey con `userVerification: required` cuenta como sesión completa (equivale a contraseña + segundo
  factor). Sin ella, el flujo actual no cambia.
- Biblioteca: `github.com/go-webauthn/webauthn` (no implementar el protocolo a mano).
- RP ID = nombre público del core (`PublicHostnames[0]`); el origen incluye el puerto del panel. Solo funciona
  por HTTPS con certificado de confianza (ya existe por delegación DNS).
- Alta de passkey: solo con sesión completa y reconfirmando la contraseña. Máximo 10 por usuario; se pueden
  nombrar y borrar desde Ajustes. Borrar la última no bloquea al usuario (queda contraseña + TOTP).
- Todo se audita (`passkey_added`, `passkey_removed`, `login_passkey`), con límite de intentos como el login.

## Pasos
1. **Núcleo** (Opus diseña, Sonnet implementa; revisión de seguridad Opus). Archivos: migración
   `0008_passkeys.sql` (tabla `passkey`: id, user_id, credential_id único, public_key, sign_count, name,
   created_at, last_used_at; sesión de ceremonia con caducidad de 5 min), `core/internal/store/passkeys.go`,
   `core/internal/api/passkeys.go`, rutas bajo `/api/v1/auth/passkey/{register,login}/{begin,finish}` y
   `GET/DELETE /api/v1/auth/passkeys`. Terminado: pruebas con autenticador simulado, contador de firmas
   comprobado (clon detectado), desafío de un solo uso, límite de intentos. Versión: menor. CHANGELOG EN+ES.
2. **Panel** (Sonnet). Botón «Entrar con huella o PIN» en `web/src/auth/Login.tsx`; gestión en Ajustes
   (añadir, nombrar, borrar). Textos en ca/es/en. Terminado: pruebas de componente y `pnpm i18n-check`.
3. **Documentación** (Sonnet/Haiku): `docs/{en,es,ca}` sección de acceso, `SECURITY`, README. EN+ES.

## Avisos
- WebAuthn exige contexto seguro: con la CA propia (sin certificado de confianza) los navegadores pueden
  rechazarlo; documentarlo.
- No tocar la lógica de TOTP ni de recuperación.
