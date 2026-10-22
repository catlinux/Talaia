# syntax=docker/dockerfile:1
#
# talaia-core in one small image: the compiled web panel and the Go binary.
# Build it from the repository root (compose does it for you):
#   docker build -f packaging/core/Dockerfile -t talaia-core:local .
#
# The core writes its CA and its secrets key under TALAIA_DATA_DIR, so that
# directory is a volume in compose and is never baked into the image.

# Stage 1: the web panel.
FROM node:24-bookworm-slim AS web

# corepack runs the pnpm version pinned in web/package.json. The prompt is off
# so the build never waits for an answer.
ENV COREPACK_ENABLE_DOWNLOAD_PROMPT=0
RUN corepack enable

WORKDIR /build

# The Vite config reads the version from ../VERSION.
COPY VERSION ./VERSION
COPY web/package.json web/pnpm-lock.yaml ./web/
# Run pnpm inside web/ so corepack picks the version pinned in its package.json.
RUN cd web && pnpm install --frozen-lockfile

COPY web ./web
RUN cd web && pnpm build

# Stage 2: the static Go binary.
FROM golang:1.26-bookworm AS build

WORKDIR /src

# The build uses the go.work of the repository, which lists agent, cli, core and
# proto. The module files of all four are enough for the workspace to resolve;
# only core and proto are copied with their sources. go.work.sum is git-ignored,
# so it is not copied.
COPY go.work ./
COPY core/go.mod core/go.sum ./core/
COPY proto/go.mod proto/go.sum ./proto/
COPY agent/go.mod agent/go.sum ./agent/
COPY cli/go.mod cli/go.sum ./cli/
RUN go mod download

COPY core ./core
COPY proto ./proto
COPY VERSION ./VERSION

# A build argument overrides the version stamped into the binary; without it the
# VERSION file of the repository is used. GIT_COMMIT is the commit the helper
# passes at build time; empty leaves the version unknown.
ARG VERSION=""
ARG GIT_COMMIT=""
RUN version="${VERSION:-$(cat VERSION)}" && commit="${GIT_COMMIT}" && \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${version} -X main.commit=${commit}" \
    -o /out/talaia-core ./core/cmd/talaia-core

# Stage 3: the agent packages the core hands to new servers (amd64 and arm64).
FROM golang:1.26-bookworm AS agentpkg

RUN go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.48.0

WORKDIR /src
COPY go.work ./
COPY core/go.mod core/go.sum ./core/
COPY cli/go.mod cli/go.sum ./cli/
COPY proto ./proto
COPY agent ./agent
COPY VERSION ./VERSION
COPY scripts/build-deb.sh ./scripts/build-deb.sh
COPY packaging/agent ./packaging/agent
COPY LICENSE THIRD-PARTY-NOTICES.txt ./

ARG VERSION=""
RUN version="${VERSION:-$(cat VERSION)}" && mkdir -p /out && \
    for arch in amd64 arm64; do \
        bash scripts/build-deb.sh "${version}" "${arch}" && \
        mv "dist/talaia-agent_${version}_${arch}.deb" "/out/talaia-agent_${arch}.deb"; \
    done

# Stage 4: the runtime. distroless has no shell, so a HEALTHCHECK is not
# possible here: compose checks the database and the core answers on its port.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/talaia-core /usr/local/bin/talaia-core
COPY --from=web /build/web/dist /usr/share/talaia/web
# install.sh and the agent packages, served at /install.sh and /agent/*.deb.
COPY --from=agentpkg /out /usr/share/talaia/dist
COPY packaging/install.sh /usr/share/talaia/dist/install.sh
# The binary and the panel bundle third-party code whose licenses require their notices.
COPY LICENSE THIRD-PARTY-NOTICES.txt /usr/share/doc/talaia/

# The nonroot user (uid 65532) owns the data volume; compose prepares it.
USER nonroot
EXPOSE 8443 8444
ENTRYPOINT ["/usr/local/bin/talaia-core"]
CMD ["serve"]
