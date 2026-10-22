# Cómo contribuir a Talaia

[Read in English](CONTRIBUTING.md)

Gracias por tu interés en Talaia. El proyecto lo crea y mantiene **[CatLinux](https://github.com/catlinux)**,
que tiene la decisión final sobre cada cambio.

## Antes de empezar

- Lee el [README](README.es.md), la [hoja de ruta](ROADMAP.es.md) y la [licencia](LICENSE) (WNCL-TP-1.0).
- Talaia está en una fase temprana. Es probable que todavía no se acepte trabajo que no forme parte de la
  fase actual de la hoja de ruta. Si tienes una idea, abre primero una incidencia.

## Informar de errores y proponer ideas

- Busca primero entre las incidencias existentes.
- Para errores, incluye: versión de Talaia (`VERSION`), sistema operativo, versión de Docker, pasos para
  reproducirlo, qué esperabas y qué ha pasado.
- Problemas de seguridad: **no abras una incidencia pública**. Sigue [SECURITY.es.md](SECURITY.es.md).

## Pull requests

1. Haz un fork del repositorio y crea una rama desde `main`: `feat/nombre-corto` o `fix/nombre-corto`.
2. Cada pull request debe centrarse en un solo cambio.
3. Sigue las normas de abajo. Si una pull request no las cumple, se pedirá que se corrija.
4. Rellena la descripción: qué cambia, por qué y cómo lo has probado.

### Normas

- **Commits:** [Conventional Commits](https://www.conventionalcommits.org/es/) en inglés, por ejemplo
  `feat(agent): send heartbeat every 10 seconds`.
- **Idiomas del software:** todo texto visible para el usuario pasa por i18n con traducciones en
  **catalán (`ca`), castellano (`es`) e inglés (`en`)**. Ver [docs/es/i18n.md](docs/es/i18n.md).
- **Idiomas de la documentación:** cualquier cambio de documentación se hace en **inglés y castellano**.
- **Registro de cambios:** añade una línea en *Sin publicar* en `CHANGELOG.md` y `CHANGELOG.es.md`.
- **Licencia:** las cabeceras de licencia se aplican con el script del mantenedor. No las escribas ni las edites a mano.
- **Atribución:** no elimines ni modifiques los avisos de copyright, `LICENSE` ni la atribución a CatLinux
  dentro de la aplicación.
- **Pruebas:** la lógica nueva debe incluir pruebas, y todas deben pasar.
- **Finales de línea:** solo LF (configurado en `.gitattributes`).

## Entorno de desarrollo

Ver [docs/es/desarrollo.md](docs/es/desarrollo.md).

## Versiones

El mantenedor decide cuándo se publica. Las versiones siguen SemVer `x.y.z`; ver la tabla del
[README](README.es.md#versiones).
