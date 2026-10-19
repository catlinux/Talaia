# Fase 1 · Tarea D · Diseño de enrolamiento, CA interna y mTLS

**Estado: implementado (E1–E3) y revisado por Opus el 2026-10-19 (ver «Revisión de seguridad»). Pendiente de la aprobación del mantenedor.** Las tareas E1–E3 lo implementan en commits locales; si la revisión lo cambia, se rehacen. No se publica ni se sube nada hasta la revisión.

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

## Revisión de seguridad (Opus, 2026-10-19)

Revisado el código real (`core/internal/{ca,api,hub,store}`, `agent/internal/enroll`), no solo este documento.

**Correcto:** token de 256 bits guardado con SHA-256 y consumido con un único `UPDATE … RETURNING`; errores de token indistinguibles; CSR validado antes de gastar el token; la CA ignora subject y SAN del CSR y exige P-256; `MaxPathLen 0`; EKU separados (el certificado del core no sirve como cliente ni el del agente como servidor); TLS 1.3; huella comparada en tiempo constante y `InsecureSkipVerify` limitado a la única petición de `/ca.pem`; el agente verifica que el certificado recibido es de su clave, de la CA y con CN = `server_id`; límites de tamaño en peticiones y respuestas; ficheros escritos de forma atómica con 0600/0700.

**A corregir** (en orden de prioridad):

| # | Gravedad | Problema | Corrección |
|---|---|---|---|
| R1 | Media | La revocación solo se comprueba al conectar: un servidor revocado con el stream abierto sigue conectado indefinidamente. | El hub vuelve a comprobar el servidor en cada `TouchServer` (cada 30 s) y cierra el stream con `PermissionDenied` si está revocado o ya no existe. |
| R2 | Media | La identidad es solo el `server_id`: tras una renovación, el certificado anterior (y su clave, si se filtró) sigue valiendo hasta 90 días. | Guardar `cert_serial` y `prev_cert_serial`; el hub y `/renew` exigen que el número de serie presentado sea el actual o el anterior (el anterior cubre el caso de que el agente caiga entre la respuesta y el guardado). Al renovar: `prev = actual`, `actual = nuevo`. |
| R3 | Baja-media | Si algo falla después de consumir el token (crear el servidor, firmar, guardar), el token queda gastado y puede quedar una fila `server` huérfana. | Enrolamiento en una sola transacción: consumir token, crear servidor, firmar y guardar el certificado; si algo falla, `ROLLBACK` y el token sigue válido. |
| R4 | Baja-media | En la renovación, la clave y el certificado se escriben en dos renombrados: una caída entre ambos deja un par que no casa y el agente ya no arranca. | Guardar clave y certificado juntos en `agent.pem` con un único renombrado atómico; leer `agent.key` + `agent.crt` solo como compatibilidad. |
| R5 | Baja | `insecure_dev` se puede activar escuchando en todas las interfaces. | Con `insecure_dev`, el core se niega a arrancar si `listen` no es una dirección de loopback; `make dev` escucha en `127.0.0.1:8080`. |
| R6 | Baja | Con un token inválido o una huella errónea el agente sale con 1 y systemd lo reinicia cada 5 s para siempre. | Salir con 78 (`EX_CONFIG`) en errores permanentes y `RestartPreventExitStatus=78` en la unidad. |

**Riesgos aceptados para esta fase** (documentados, a revisar más adelante): la clave de la CA está sin cifrar en disco (0600) y en la memoria del proceso expuesto; un agente que pase más de 90 días sin conectar tiene que volver a enrolarse; no hay rotación de la CA; `/ca.pem` es público (es un certificado, no un secreto).
