# Catálogo detallado y arreglos tras la prueba real (2026-10-31)

Origen: prueba del mantenedor con el core real en 0.6.22.

1. Ajustes recarga la página cuando el core vuelve con otro commit tras actualizar (el bundle viejo y la
   lista de servidores quedaban desfasados, y no aparecía «Actualizar agente»). AgentRelay, esfuerzo
   medio. 0.6.23 parche. Corregido.
2. Ficha ampliada: campos nuevos por entrada (`details`, `stop`, `problems[]`), órdenes de diagnóstico
   generadas en código y `checks`/`docs` en `catalog.ts`; botón «Ver ficha completa» en el ⓘ que abre un
   Drawer. AgentRelay, esfuerzo medio. 0.6.24 menor de fase → parche. Añadido.
3. Pantalla `/catalog` (Catálogo) en Administración: búsqueda, filtros por tipo, categoría e importancia,
   fila abre el Drawer del paso 2; entrada en la paleta. AgentRelay, esfuerzo medio. 0.6.25. Añadido.
4. Contenido: entradas nuevas (servicios y imágenes reales de la flota: certbot servicio, anacron,
   logrotate, firewalld, NetworkManager, lightdm, jellyfin, opendkim, timescaledb, node, busybox,
   immich-postgres…) y textos `details`/`stop`/`problems` en ca/es/en para todas. En tandas de ~15
   entradas, sin subir versión; la última tanda sube 0.6.26. AgentRelay, esfuerzo medio.
5. Repaso del orquestador: capturas, docs `panel.md` EN/ES, TODO.

Estado: ver marcas ✅ en cada paso.
