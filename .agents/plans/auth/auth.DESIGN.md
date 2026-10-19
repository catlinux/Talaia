# Autenticación del panel (usuario único con 2FA)

Estado: diseño de Opus (2026-10-19), aprobado por el mantenedor («jo em deixo assessorar per tu»). Adelanta el punto «Single user with 2FA (TOTP) and audit log» de la fase 3; la fase 4 añadirá usuarios, passkeys y OIDC **encima** de esto, sin sustituirlo.

## Principios

- Simple: un único administrador, contraseña + TOTP obligatorio, sesiones de servidor con cookie.
- Ningún secreto se guarda en claro: contraseña con argon2id, tokens de sesión y códigos de recuperación con SHA-256, secreto TOTP cifrado con AES-256-GCM.
- Errores indistinguibles hacia fuera; el detalle va al registro de auditoría.
- El alta del administrador solo desde la consola del servidor, nunca desde la web.

## Modelo de datos (migración 0004)

- `app_user`: `id` uuid, `username` text único, `password_hash` text (PHC argon2id), `totp_secret_enc` bytea, `totp_enabled_at` timestamptz, `totp_last_step` bigint, `failed_attempts` int, `locked_until` timestamptz, `password_changed_at`, `created_at`.
- `recovery_code`: `id`, `user_id`, `code_hash` bytea, `used_at`.
- `session`: `id` uuid, `token_hash` bytea único, `user_id`, `stage` text (`mfa_pending` | `full`), `created_at`, `last_seen_at`, `expires_at`, `revoked_at`, `ip`, `user_agent`.
- `audit_event`: `id`, `at`, `actor`, `action`, `ip`, `result`, `details` jsonb (append-only: el código nunca hace UPDATE ni DELETE).

## Criptografía

| Qué | Cómo |
|---|---|
| Contraseña | argon2id (`golang.org/x/crypto/argon2`), m = 64 MiB, t = 3, p = 1, sal de 16 bytes, formato PHC; como mucho 2 cálculos a la vez (semáforo) para no agotar la RAM de un VPS pequeño. Mínimo 12 caracteres, máximo 256. Con un usuario inexistente se calcula igualmente un hash de relleno (mismo tiempo de respuesta). |
| TOTP | RFC 6238: SHA-1, 30 s, 6 dígitos, ventana ±1 paso, se rechaza cualquier paso ≤ `totp_last_step` (sin reutilización). Secreto de 20 bytes. |
| Cifrado del secreto TOTP | AES-256-GCM con una clave aleatoria en `data_dir/secrets.key` (0600, creada al primer uso). |
| Token de sesión | 32 bytes aleatorios en base64url; en la base de datos solo su SHA-256. |
| Códigos de recuperación | 10 códigos de 10 caracteres base32 (50 bits), SHA-256, un solo uso. |

## Flujo

1. **Alta** (consola del servidor): `talaia-core user create --username <u>` pide la contraseña dos veces sin eco (o `--password-stdin`). Falla si ya existe un usuario (fase 3: uno solo).
2. **Login**: `POST /api/v1/auth/login {username, password}` → si es correcta, sesión `mfa_pending` de 5 minutos y respuesta `{next: "totp_setup" | "totp"}`. Si no: `401 {"error":{"code":"invalid_credentials"}}`.
3. **Primer acceso**: `POST /api/v1/auth/totp/setup` (solo `mfa_pending` y TOTP sin activar) devuelve el secreto base32 y el URI `otpauth://`; `POST /api/v1/auth/totp/confirm {code}` lo activa, devuelve los 10 códigos de recuperación **una sola vez** y sube la sesión a `full`.
4. **Accesos siguientes**: `POST /api/v1/auth/totp {code}` o `{recovery_code}` → `full`.
5. **Al pasar a `full` se cambia el token** (nuevo token, el anterior se revoca): evita la fijación de sesión.
6. `GET /api/v1/auth/session`, `POST /api/v1/auth/logout`, `POST /api/v1/auth/password {current_password, new_password, code}` (pide también un código TOTP; cierra las demás sesiones).
7. Toda ruta `/api/v1/*` que no sea de autenticación exige una sesión `full`.
8. Recuperación por consola: `talaia-core user reset-2fa` y `talaia-core session revoke-all`.

## Sesión y navegador

- Cookie `__Host-talaia_session`: `Secure`, `HttpOnly`, `SameSite=Strict`, `Path=/`, sin `Domain`.
- Caducidad: 12 h sin actividad y 7 días como máximo (`mfa_pending`: 5 min). `last_seen_at` se actualiza como mucho una vez por minuto.
- CSRF: además de `SameSite=Strict`, toda petición que modifica exige `Content-Type: application/json` y un `Origin` (o `Sec-Fetch-Site: same-origin`) que esté en `allowed_origins` (por defecto `https://<public_hostnames>`; en `insecure_dev` también `http://localhost:5173` y `http://127.0.0.1:5173`). Si no, 403.
- Cabeceras en todas las respuestas: `Content-Security-Policy: default-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, `Cache-Control: no-store` en la API, y `Strict-Transport-Security: max-age=31536000` con TLS.
- El core sirve el panel compilado desde `web_dir` (por defecto `/usr/share/talaia/web`): mismo origen, sin cookies de terceros. En desarrollo, Vite hace de proxy de `/api` al core.

## Protección contra fuerza bruta

- Por IP: 10 intentos de login o de código cada 15 minutos (`RemoteAddr`, sin fiarse de cabeceras de proxy).
- Por usuario: 5 fallos seguidos (contraseña o código) → bloqueo de 15 minutos; la respuesta sigue siendo `invalid_credentials`.
- Una sesión `mfa_pending` se invalida tras 5 códigos erróneos.

## Auditoría

`user_created`, `login_success`, `login_failure`, `mfa_enabled`, `mfa_failure`, `recovery_code_used`, `logout`, `password_changed`, `sessions_revoked`, `mfa_reset`, con IP y resultado. Nunca contraseñas, códigos ni tokens.

## Fuera de alcance

Varios usuarios, roles, passkeys y OIDC (fase 4); código QR en el alta del TOTP (de momento, secreto y enlace `otpauth://`, que todas las apps aceptan a mano); avisos por correo.

## Tareas

| # | Alcance | Archivos | Ejecutor |
|---|---|---|---|
| A1 ✅ | Migración 0004, paquete `core/internal/auth` (argon2id, TOTP con los vectores del RFC 6238, tokens, AES-GCM con `secrets.key`), métodos del store (usuarios, sesiones, recuperación, auditoría) | `core/internal/auth/**`, `core/internal/store/**` | AgentRelay, alto |
| A2 ✅ | API de autenticación, middleware de sesión, CSRF/Origin, cabeceras, límites, auditoría, `web_dir`, `allowed_origins`, comandos `user create`, `user reset-2fa`, `session revoke-all` | `core/internal/api/**`, `core/internal/config/**`, `core/cmd/**` | AgentRelay, alto |
| A3 | Panel: cliente de autenticación, pantallas de login, alta y verificación del TOTP, códigos de recuperación, cierre de sesión, protección de rutas, i18n ca/es/en, proxy de Vite | `web/**` | AgentRelay, medio |
| A4 | Prueba real extremo a extremo y documentación EN/ES | docs | Orquestador |
