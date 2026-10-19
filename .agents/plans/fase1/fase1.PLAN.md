# Fase 1 · Agente, conexión e inventario (0.2.0)

Estado: **iniciada por petición expresa del mantenedor (2026-10-19) antes de cerrar la fase 0** (faltan `make dev` en Docker, `license-check` y los VPS de prueba). Todo va en commits locales; nada de esta fase se sube sin su aprobación. Si prefiere esperar, se pueden revertir los commits `feat(...)` posteriores a `docs: record end-to-end verification`.

Criterio: los VPS reales aparecen en la flota con métricas y sobreviven a un reinicio del core.

| # | Tarea | Quién | Archivos | Terminada cuando |
|---|---|---|---|---|
| A ✅ | Inventario de SO/hardware y métricas reales (CPU, RAM, disco, red); campos de red en `Metrics` | Orquestador (proto) + ejecutor medio | `proto/**`, `agent/internal/collect/**`, `agent/internal/link/**` | pruebas del agente en verde; el hub muestra los valores |
| B ✅ | Paquete `.deb` del agente (nfpm), unidad systemd, `install.sh` con `--token` (sin enrolamiento real aún) | Ejecutor medio | `packaging/agent/**`, `scripts/build-deb.sh` | el `.deb` se construye y `dpkg-deb -I/-c` lo valida en el WSL |
| C ✅ | Pantalla de flota (maqueta 1) con datos de ejemplo tipados tras una interfaz de cliente de API | Ejecutor medio | `web/src/screens/Fleet*`, `web/src/api/**`, `web/src/locales/*/fleet.json` | `pnpm build/lint/format/test` en verde, i18n-check en verde |
| D | **Diseño de enrolamiento, CA interna y mTLS** (token de un solo uso, CSR, certificado 90 días, renovación, revocación) | **Orquestador con Opus** | `docs/*/architecture.md`, `proto/**` | diseño aprobado por el mantenedor |
| E | Implementar D en core y agente | Ejecutor alto, tras D | `core/internal/ca/**`, `core/internal/enroll/**`, `agent/internal/enroll/**` | pruebas de enrolamiento y rechazo de certificados no válidos |
| F | Persistencia de servidores (tabla `server`, `last_seen`, estado) y API REST de lectura; supervivencia al reinicio del core | Ejecutor medio, tras E | `core/internal/store/**`, `core/internal/api/**` | reinicio del core sin perder servidores |
| G | Inventario de contenedores, proyectos Compose y servicios systemd | Ejecutor alto | `agent/internal/inventory/**` | inventario correcto en el Debian de pruebas |
| H | Conectar la pantalla de flota a la API real | Ejecutor medio, tras F | `web/src/api/**` | flota real en el panel |

Las tareas D–H necesitan el mantenedor para aprobar el diseño (D) o un VPS real. Reglas de siempre: tareas con archivos disjuntos, validaciones compatibles con `cmd.exe`, revisión propia del diff y de las pruebas antes de aceptar.
