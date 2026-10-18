# Flujo de desarrollo

[Read in English](../en/development.md) · [Volver al README](../../README.es.md)

## 1. El ciclo

```
┌──────────────────────────────┐   commit    ┌──────────────────┐
│ Windows                      │ ──────────▶ │ repositorio local│
│ VS Code + Claude Code        │             └────────┬─────────┘
│ + ejecutores con Agent Relay │                      │ git push (solo cuando el
└──────────────────────────────┘                      │ mantenedor lo aprueba)
                                                      ▼
                                               ┌──────────────┐
                                               │    GitHub    │
                                               │ catlinux/    │
                                               │ Talaia       │
                                               └──────┬───────┘
                                                      │ git clone / git pull
                                                      ▼
                                        ┌─────────────────────────────┐
                                        │ Equipo Linux de pruebas     │
                                        │ Debian 13: compilar y probar│
                                        └─────────────────────────────┘
```

1. Se trabaja en local en Windows con VS Code y Claude Code.
2. Claude Code (orquestador) divide el trabajo y lo delega a agentes ejecutores mediante Agent Relay.
3. Los cambios se guardan con commits locales. Se pueden hacer commits en cualquier momento.
4. **El mantenedor aprueba cada push.** Los agentes nunca hacen push por su cuenta.
5. En el equipo Linux, `git pull` y a probar.

## 2. Equipo Windows

Se han eliminado las instrucciones de Windows: Claude Code ya tiene las directrices de trabajo en local, en remoto y para los repositorios de GitHub.

> `.gitattributes` fuerza finales de línea LF en todos los archivos de texto, así los archivos editados en
> Windows funcionan bien en Linux.

Opcional: WSL2 con Debian permite compilar y probar en Linux en el mismo equipo Windows.

## 3. Equipo Linux de pruebas

Debian 13.

```bash
sudo apt update
sudo apt install -y git make curl ca-certificates
# Docker Engine: https://docs.docker.com/engine/install/debian/
# Go y Node.js: versiones fijadas en la fase 0

git clone https://github.com/catlinux/Talaia.git
cd Talaia
```

Para traer los cambios nuevos:

```bash
git pull
```

Los comandos de prueba se documentarán aquí a partir de la fase 0 (`make dev`, `make test`).

## 4. Ramas

- `main` siempre funciona.
- Mientras haya un solo mantenedor, los cambios pequeños pueden ir directamente a `main`.
- Cambios grandes: ramas `feat/<nombre>` o `fix/<nombre>`, que se fusionan con `main` cuando pasan las
  comprobaciones.

## 5. Publicación de versiones

1. Todas las tareas de la fase están marcadas en `TODO.md` / `TODO.es.md` y se cumple el criterio de
   «terminada cuando».
2. Actualiza `VERSION`.
3. En los dos changelogs, mueve *Sin publicar* a una sección nueva `[x.y.z] - AAAA-MM-DD`.
4. Commit: `chore(release): x.y.z`.
5. El mantenedor crea la etiqueta `vx.y.z` y hace push.

## 6. Trabajar con agentes de IA

- [CLAUDE.md](../../CLAUDE.md): normas del orquestador (Claude Code).
- [AGENTS.md](../../AGENTS.md): normas de los agentes ejecutores.

El orquestador da a cada ejecutor un encargo con un conjunto de archivos propio, revisa cada resultado y
hace los commits. Los ejecutores nunca hacen commit ni push.
