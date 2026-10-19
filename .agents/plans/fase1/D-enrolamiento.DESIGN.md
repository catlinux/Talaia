# Fase 1 · Tarea D · Diseño de enrolamiento, CA interna y mTLS

**Estado: BORRADOR escrito por Sonnet en ausencia del mantenedor (2026-10-19). Pendiente de revisión de seguridad (recomendado: Opus) y de aprobación del mantenedor.** Las tareas E1–E3 lo implementan en commits locales; si la revisión lo cambia, se rehacen. No se publica ni se sube nada hasta la revisión.

## Objetivos

- Un agente prueba que se le autorizó (token de un solo uso) y recibe una identidad propia (certificado de cliente).
- Toda la comunicación agente↔core va por TLS con autenticación mutua; el agente solo confía en la CA de su core.
- Sin puertos abiertos en el VPS: sigue siendo el agente quien conecta.

## Piezas

1. **CA interna** (ECDSA P-256), creada en el primer arranque del core si no existe. Archivos en `data_dir` (por defecto `/var/lib/talaia/ca/`): `ca.crt` (10 años) y `ca.key` (0600). *Pendiente futuro:* cifrar `ca.key` en reposo (age) cuando exista la caja de secretos.
2. **Certificado del servidor core**: lo firma la CA, SAN = `public_hostnames` de la configuración (obligatorio si TLS está activo), validez 1 año, renovado al arrancar si caduca en menos de 30 días.
3. **Tokens de enrolamiento**: 32 bytes aleatorios (crypto/rand), formato `tl_enr_<base64url>`. En la base de datos solo se guarda el SHA-256 (tabla `enrollment_token`: `id`, `token_hash`, `created_at`, `expires_at` = +15 min, `used_at`, `server_name`). Se crean con `talaia-core token create --name <nombre>` (no hay panel con autenticación hasta la fase 3); el comando imprime el token **una sola vez**, junto con la huella de la CA.
4. **Huella de la CA** (SHA-256 del DER de `ca.crt`, hex). Va en la orden de instalación (`--ca-fingerprint`). Evita la confianza ciega en el primer contacto (mismo modelo que `kubeadm --discovery-token-ca-cert-hash`).
5. **Servidor**: tabla `server` (`id` uuid, `name`, `cert_serial`, `cert_not_after`, `created_at`, `last_seen`, `revoked_at`).

## Flujo de alta

1. El mantenedor ejecuta `talaia-core token create --name vps-web-01` → `token`, `ca-fingerprint`.
2. En el VPS: `install.sh --url https://core.example.com --token <t> --ca-fingerprint <h> --deb …`. Se guardan en `/etc/talaia/enroll.token` (0600) y en `agent.toml` (`ca_fingerprint`).
3. Al arrancar sin certificado, el agente:
   a. pide `GET /ca.pem` al core con verificación TLS desactivada **solo para esta petición**, calcula el SHA-256 y exige que coincida con `ca_fingerprint`; si no coincide, aborta (no reintenta con otro destino);
   b. genera clave ECDSA P-256 en `/etc/talaia/certs/agent.key` (0600) y un CSR;
   c. `POST /enroll` (TLS verificado contra la CA recién confiada) con `{token, csr_pem, hostname}`;
   d. el core valida el token y lo consume **atómicamente** (`UPDATE … SET used_at = now() WHERE token_hash = $1 AND used_at IS NULL AND expires_at > now() RETURNING …`), crea la fila `server`, firma el certificado de cliente (CN = `server_id`, `ExtKeyUsage = ClientAuth`, validez 90 días; se ignora el subject del CSR y se exige clave ECDSA P-256) y responde `{server_id, cert_pem, ca_pem}`;
   e. el agente guarda `agent.crt` y `ca.crt`, borra `enroll.token` y abre el stream mTLS.
4. Respuestas de error genéricas (`invalid_token`) sin distinguir caducado/usado/inexistente. Límite de 10 intentos por minuto y por IP en `/enroll`.

## Transporte

- El core sirve HTTPS (HTTP/2 y HTTP/1.1) en `listen` con `tls.Config{MinVersion: TLS13, ClientAuth: VerifyClientCertIfGiven, ClientCAs: CA}`. `/healthz`, `/ca.pem` y `/enroll` no exigen certificado de cliente; **la ruta del hub sí**: el manejador rechaza (401) si no hay certificado válido, y la identidad se toma de `CN = server_id` (se acabó clavar por hostname).
- Al conectar, el hub comprueba en la base de datos que el servidor existe y no está revocado (`revoked_at IS NULL`); si no, cierra con `PermissionDenied`.
- **Modo desarrollo sin TLS**: solo con `insecure_dev = true` (o `TALAIA_INSECURE_DEV=1`) en el core y `insecure_dev` en el agente; imprime un aviso en cada arranque. El valor por defecto es seguro (TLS). `make dev` lo activa.

## Renovación y revocación

- Renovación a los 2/3 de la vida del certificado: `POST /renew` por mTLS con un CSR nuevo; el core responde con un certificado nuevo para el mismo `server_id` (la identidad sale del certificado presentado, no del cuerpo). El agente recarga el certificado sin reiniciar el stream hasta la próxima reconexión.
- Revocación: `talaia-core server revoke <id>` marca `revoked_at`. Los certificados duran 90 días y el hub comprueba la revocación en cada conexión y cada `/renew`; no hay CRL.

## Fuera de alcance (fases posteriores)

Cifrado en reposo de `ca.key`, rotación de la CA, panel web para crear tokens (fase 3), autenticación de usuarios (fase 3), auditoría de enrolamientos (fase 3).

## Plan de implementación

| Tarea | Alcance | Archivos |
|---|---|---|
| E1 ✅ | `core/internal/ca` (generar/cargar CA, firmar server/cliente, huella), migración `server` + `enrollment_token`, comando `token create` y `server revoke` | `core/internal/ca/**`, `core/internal/store/**`, `core/cmd/talaia-core/**` |
| E2 ✅ | TLS en el core, `/ca.pem`, `/enroll`, `/renew`, identidad por certificado en el hub, modo `insecure_dev` | `core/internal/api/**`, `core/internal/hub/**`, `core/internal/config/**`, `core/cmd/talaia-core/**` |
| E3 ✅ | Agente: descubrimiento con huella, CSR, enrolamiento, mTLS, renovación, `insecure_dev` | `agent/internal/enroll/**`, `agent/internal/link/**`, `agent/internal/config/**`, `agent/cmd/**` |
