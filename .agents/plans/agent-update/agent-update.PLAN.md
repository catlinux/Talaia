# Actualizar los agentes desde el panel

Objetivo: que el administrador vea la versión de cada agente y pueda actualizarlo con un botón, sin ir al
servidor a pegar un comando. Hoy el core sirve el `.deb` (`/agent/talaia-agent_<arch>.deb`) y el agente ya
conoce la CA del core; falta una orden tipada.

## Diseño (a confirmar por el mantenedor: ejecuta código como root en cada servidor)
- Orden tipada nueva en `proto/agent.proto`: `AgentUpdate { string version = 1; string sha256 = 2; }` en
  `CoreMsg.cmd` (número libre ≥ 22; nunca renumerar). El core rellena `sha256` con el hash del `.deb` que
  sirve y `version` con la suya; el agente no acepta nada que no coincida con ese hash.
- Agente (`agent/internal/jobs/update.go`): descarga `<core_url>/agent/talaia-agent_<arch>.deb` con su
  identidad mTLS y la CA guardada, limita el tamaño (64 MiB), comprueba el SHA-256 recibido en la orden,
  rechaza versiones iguales o anteriores, lo guarda en `/var/lib/talaia/update/` (0700) y lanza la
  instalación **desacoplada** con `systemd-run --unit=talaia-agent-upgrade --collect apt-get install -y
  --reinstall <ruta>` para que sobreviva al reinicio del propio agente. Informa `RUNNING` antes de lanzarla;
  el éxito lo ve el core cuando el agente vuelve con la versión nueva.
- Core: `POST /api/v1/servers/{id}/agent/update` (sesión completa, `confirm`), auditoría `agent_update`,
  una sola actualización en vuelo por servidor, comprobación de que el agente está conectado y tiene
  versión menor. Endpoint «actualizar todos» que las encola de una en una.
- Panel: la tarjeta del servidor muestra la versión del agente y un aviso «Actualización disponible»; botón
  en la pantalla del servidor y «Actualizar agentes» en Ajustes → Servidores. ca/es/en.
- Segunda parte (después): orden tipada para cambiar la URL del core en el agente (`SetCoreURL`), con la
  huella de la CA nueva; el agente la prueba antes de guardarla y vuelve a la antigua si no conecta.

## Pasos
1. Proto + agente + pruebas (Sonnet alto; revisión de seguridad Opus).
2. Core: endpoint, auditoría, cola y límites.
3. Panel + documentación EN/ES/CA. Versión: menor (nueva funcionalidad dentro de la fase 2: parche según
   la política vigente, 0.2.x).

## Riesgos a revisar
- Quien controle el core controla el `.deb`: ya es así hoy con el comando de instalación; el hash solo evita
  cambios en tránsito y errores, no protege de un core comprometido (documentarlo en `docs/*/security.md`).
- Un `.deb` roto deja el agente caído: conservar el anterior en `/var/lib/talaia/update/previous.deb` y
  documentar la vuelta atrás manual.
