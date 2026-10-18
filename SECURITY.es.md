# Política de seguridad

[Read in English](SECURITY.md)

Talaia gestiona servidores con privilegios de root. Los problemas de seguridad se toman en serio.

## Versiones soportadas

Talaia todavía no ha publicado ninguna versión estable. Hasta la 1.0.0, solo la última versión de `main`
recibe correcciones de seguridad.

| Versión | Soportada |
|---|---|
| último `main` | ✅ |
| anteriores | ❌ |

## Cómo informar de una vulnerabilidad

**No abras una incidencia pública.**

Infórmala de forma privada a través de GitHub: ve a la pestaña **Security** del repositorio y elige
**Report a vulnerability**
(<https://github.com/catlinux/Talaia/security/advisories/new>).

Incluye, por favor:

- componente afectado (`core`, `agent`, `web`, `cli`, empaquetado…) y versión o commit;
- descripción del problema y su impacto;
- pasos para reproducirlo o una prueba de concepto;
- cualquier propuesta de corrección.

## Qué puedes esperar

- Acuse de recibo en un plazo de 7 días.
- Una valoración y un plan en un plazo de 30 días.
- Mención en las notas de la versión si lo deseas, una vez publicada la corrección.

## Alcance

Dentro del alcance: todo lo que hay en este repositorio. Fuera del alcance: vulnerabilidades de software
de terceros (Docker, paquetes de Debian, Caddy…), salvo que Talaia lo use de forma insegura.
