# Hoja de ruta de Talaia

[Read in English](ROADMAP.md) · [Volver al README](README.es.md) · [Versión visual con maquetas](docs/design/talaia-roadmap.html)

Esta hoja de ruta divide el proyecto en **10 fases**. Cada fase termina con algo utilizable en servidores
reales y tiene un criterio concreto de **«terminada cuando»**. Ninguna fase empieza hasta que la anterior
cumple su criterio.

Las estimaciones suponen **una persona dedicando 15–20 horas por semana**. Total: unas **38 semanas**.

```
Semana 0    2        6          11         16      19      22           28      31        35      38
       ├─F0─┼───F1───┼────F2────┼────F3────┼───F4──┼───F5──┼─────F6─────┼───F7──┼────F8───┼───F9──┤
       0.1.0  0.2.0     0.3.0      0.4.0 MVP  0.5.0   0.6.0    0.7.0      0.8.0    0.9.0    1.0.0
```

| Fase | Nombre | Semanas | Versión | Estado |
|---|---|---|---|---|
| — | Definición del proyecto y documentación | — | 0.0.x | 🟢 Terminada |
| 0 | Cimientos | 1–2 | 0.1.0 | 🟢 Terminada |
| 1 | Agente, conexión e inventario | 3–6 | 0.2.0 | 🟢 Terminada |
| 2 | Contenedores y stacks | 7–11 | 0.3.0 | 🟢 Terminada |
| 3 | Servidor entero (**MVP**) | 12–16 | 0.4.0 | 🟢 Terminada |
| 4 | Usuarios, permisos y auditoría | 17–19 | 0.5.0 | 🟡 En curso |
| 5 | Monitorización y alertas | 20–22 | 0.6.0 | ⚪ Pendiente |
| 6 | Despliegues, dominios y plantillas | 23–28 | 0.7.0 | ⚪ Pendiente |
| 7 | Copias de seguridad | 29–31 | 0.8.0 | ⚪ Pendiente |
| 8 | Flota y automatización | 32–35 | 0.9.0 | ⚪ Pendiente |
| 9 | Versión 1.0 | 36–38 | 1.0.0 | ⚪ Pendiente |

Leyenda: ⚪ pendiente · 🟡 en curso · 🟢 terminada

---

## Fase 0 · Cimientos (semanas 1–2) → 0.1.0

Preparar el terreno para que todo lo demás vaya rápido.

- Estructura de monorepo, CI con pruebas y lint (GitHub Actions)
- Contrato `proto/` v0 y generación de código
- Esqueleto del núcleo con PostgreSQL y migraciones
- Esqueleto del panel web, tokens de diseño, tema claro/oscuro
- **i18n preparado desde el inicio**: i18next en el panel, go-i18n en Go, archivos de idioma
  `ca`/`es`/`en` y una comprobación en CI que falla si falta una clave en algún idioma
- Script de licencia WNCL-TP-1.0 del mantenedor integrado en CI
- Dos VPS de pruebas desechables (Debian 12 y Debian 13)
- Decisiones abiertas cerradas: PostgreSQL + TimescaleDB, React, Go 1.26, Node.js 24 LTS + pnpm

**Terminada cuando:** `make dev` arranca núcleo + panel + un agente vacío en local, y el CI está en verde.

## Fase 1 · Agente, conexión e inventario (semanas 3–6) → 0.2.0

El corazón del sistema. Si esto es sólido, el resto es añadir módulos.

- Alta con token de un solo uso, CA interna, mTLS
- Túnel gRPC con reconexión, espera exponencial y heartbeat
- Inventario: SO, hardware, contenedores, proyectos Compose, servicios
- Métricas en directo (CPU, RAM, disco, red)
- Pantalla de flota (maqueta 1)
- Paquete `.deb`, unidad systemd e `install.sh`

**Terminada cuando:** tus VPS reales aparecen en la flota con métricas y sobreviven a un reinicio del
núcleo.

## Fase 2 · Contenedores y stacks (semanas 7–11) → 0.3.0

El sustituto de Portainer y Dockge.

- Contenedores: arrancar, parar, reiniciar, eliminar, logs, estadísticas, terminal
- Stacks Compose guardados en `/opt/talaia/stacks/<nombre>/`
- Editor de `compose.yaml` y `.env` con validación
- Adoptar contenedores y proyectos Compose existentes
- Imágenes, volúmenes, redes y limpieza
- Pantalla de stack (maqueta 3)

**Terminada cuando:** puedes dejar de usar Portainer para todo lo que haces hoy.

## Fase 3 · Servidor entero (semanas 12–16) → 0.4.0 · MVP

El sustituto de Cockpit, para muchos servidores.

- systemd: estado, arrancar/parar, habilitar/deshabilitar
- journald con filtros y búsqueda
- apt: actualizaciones pendientes, de seguridad, actualizar, «reinicio necesario»
- Terminal del anfitrión
- Usuario único con 2FA (TOTP) y registro de auditoría
- Pantalla de servidor (maqueta 2)

**Terminada cuando:** pasas una semana entera sin entrar por SSH a tus VPS.

> **Hito MVP (~4 meses).** Talaia ya es tu herramienta diaria. A partir de aquí, cada fase añade potencia.

## Fase 4 · Usuarios, permisos y auditoría (semanas 17–19) → 0.5.0

Abrir la herramienta a otras personas con seguridad.

- Usuarios, equipos, roles y ámbitos (organización → grupo → servidor → stack → contenedor)
- Passkeys (WebAuthn) y SSO OIDC
- Grabación de sesiones de terminal
- Claves de API con permisos y primera versión de `talaiactl`
- Pantalla de permisos (maqueta 5)

**Terminada cuando:** un usuario con acceso a un solo stack no puede ver ni tocar nada más, verificado con
pruebas automáticas.

## Fase 5 · Monitorización y alertas (semanas 20–22) → 0.6.0

Que Talaia te avise antes que tus usuarios.

- Historial de métricas en TimescaleDB con agregación
- Reglas de alerta y silenciado
- Comprobaciones HTTP/TCP y caducidad de certificados
- Notificaciones: Telegram, ntfy, correo, Discord, webhook

**Terminada cuando:** si se para un contenedor o un disco pasa del 90 %, recibes el aviso en menos de un
minuto.

## Fase 6 · Despliegues, dominios y plantillas (semanas 23–28) → 0.7.0

La parte de Coolify.

- Stacks desde Git y webhooks de push
- Builds con Dockerfile y Nixpacks
- Despliegues sin cortes y reversión
- Caddy gestionado: dominios + SSL automático
- Catálogo de plantillas (15 apps iniciales)
- Asistente de nueva app (maqueta 4)

**Terminada cuando:** hacer push a `main` publica la app con HTTPS sin hacer nada más.

## Fase 7 · Copias de seguridad (semanas 29–31) → 0.8.0

La función que esperas no necesitar nunca, pero que tiene que funcionar.

- Planes con restic para volúmenes y carpetas
- Volcados coherentes de bases de datos (PostgreSQL, MariaDB)
- Destinos: S3, Backblaze B2, SFTP
- Restaurar desde el panel

**Terminada cuando:** has restaurado una base de datos real en un servidor nuevo desde el panel.

## Fase 8 · Flota y automatización (semanas 32–35) → 0.9.0

La parte de Komodo y Ansible.

- Acciones en bloque por grupo
- Procedimientos programados (pasos encadenados)
- Cortafuegos, usuarios SSH, cron, explorador de archivos
- Auditoría de bastionado
- Sincronización GitOps (archivos TOML en un repositorio Git)

**Terminada cuando:** puedes reconstruir toda la configuración de Talaia desde un repositorio Git.

## Fase 9 · Versión 1.0 (semanas 36–38) → 1.0.0

Pulir, documentar y publicar.

- Revisión de seguridad y prueba de carga (50 agentes)
- Autoactualización firmada del agente
- Documentación de usuario y guía de instalación (EN/ES), interfaz totalmente traducida (CA/ES/EN)
- Soporte arm64, paquetes e imágenes firmados

**Terminada cuando:** otra persona lo instala siguiendo solo la documentación.

---

## Después de la 1.0

- Soporte de Podman
- Agentes para la familia RHEL (Rocky, Alma, Fedora) y Alpine
- Entornos de prueba por pull request
- Página de estado pública
- Ejecución de playbooks de Ansible
- Creación de VPS mediante API de proveedores (Hetzner, OVH…)
- Pruebas de restauración mensuales automáticas

## Riesgos

| Riesgo | Mitigación |
|---|---|
| **Querer abarcarlo todo** (el mayor riesgo) | No empezar nunca una fase antes de que la anterior cumpla su criterio. Las ideas nuevas van a [TODO.es.md](TODO.es.md), en *Ideas*. |
| **Una llave para todos los servidores** | 2FA obligatorio, panel detrás de una VPN, auditoría, módulos desactivables por servidor. |
| **Mantenimiento de plantillas** | Pocas plantillas y bien probadas; catálogos externos para el resto. |
| **El agente se ejecuta como root** | Solo órdenes tipadas, módulos desactivables, actualizaciones firmadas. |
