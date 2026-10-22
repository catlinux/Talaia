# scripts/

Maintenance scripts for the Talaia repository. · Scripts de mantenimiento del repositorio de Talaia.

| Script | Purpose / Propósito | Status / Estado |
|---|---|---|
| License header script / Script de cabeceras de licencia | Applies the WNCL-TP-1.0 license headers / Aplica las cabeceras de la licencia WNCL-TP-1.0 | Pending: provided by the maintainer / Pendiente: lo aporta el mantenedor |
| `third-party-notices.mjs` | Writes `THIRD-PARTY-NOTICES.txt` from the Go and web dependencies; `--check` fails if it is out of date / Genera `THIRD-PARTY-NOTICES.txt` a partir de las dependencias Go y web; `--check` falla si está desfasado | Done / Hecho |
| `bump-version.sh` | Updates `VERSION` and both changelogs / Actualiza `VERSION` y los dos changelogs | Phase 0 / Fase 0 |
| `i18n-check` | Fails if a key is missing in `ca`, `es` or `en` / Falla si falta una clave en `ca`, `es` o `en` | Phase 0 / Fase 0 |

All scripts use LF line endings and `#!/usr/bin/env bash`.
Todos los scripts usan finales de línea LF y `#!/usr/bin/env bash`.
