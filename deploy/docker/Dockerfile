# syntax=docker/dockerfile:1.7
#
# Tailwatch container image. Build from the repository root:
#   docker build -f deploy/docker/Dockerfile -t tailwatch .
#   (or: make docker)
#
# Stages:
#   web   — node:22-alpine builds the React UI into web/dist
#   build — golang:1.26-alpine compiles the hub (with the embedded UI) and the agent,
#           statically (CGO_ENABLED=0), stripped, reproducible (-trimpath)
#   final — gcr.io/distroless/static-debian12:nonroot: no shell, no package
#           manager, runs as uid 65532
#
# There is deliberately no HEALTHCHECK: distroless has no shell or curl to run
# one. Probe GET http://<tailscale-ip>:8484/healthz from the host or an
# orchestrator instead (see deploy/docker/docker-compose.yml and docs/DEPLOY.md).

ARG NODE_IMAGE=node:22-alpine
ARG GO_IMAGE=golang:1.26-alpine
ARG RUNTIME_IMAGE=gcr.io/distroless/static-debian12:nonroot

# ---------------------------------------------------------------------------
FROM ${NODE_IMAGE} AS web
WORKDIR /src/web
ENV npm_config_update_notifier=false
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm \
    npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---------------------------------------------------------------------------
FROM ${GO_IMAGE} AS build
ARG VERSION=dev
ARG COMMIT=unknown
ARG DATE=unknown
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-mod=readonly GOTOOLCHAIN=auto
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download
COPY . .
# The UI built in the previous stage replaces whatever the build context had.
COPY --from=web /src/web/dist ./web/dist
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    set -eux; \
    MODULE=github.com/evilgenius79/fable-tailscale; \
    LDFLAGS="-s -w -X ${MODULE}/internal/version.Version=${VERSION} -X ${MODULE}/internal/version.Commit=${COMMIT} -X ${MODULE}/internal/version.Date=${DATE}"; \
    GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -trimpath -ldflags "${LDFLAGS}" -o /out/tailwatch ./cmd/tailwatch; \
    GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build -trimpath -ldflags "${LDFLAGS}" -o /out/tailwatch-agent ./cmd/tailwatch-agent; \
    mkdir -p /out/data

# ---------------------------------------------------------------------------
FROM ${RUNTIME_IMAGE} AS final
ARG VERSION=dev
LABEL org.opencontainers.image.title="Tailwatch" \
      org.opencontainers.image.description="Tailscale network viewer and watchdog" \
      org.opencontainers.image.source="https://github.com/evilgenius79/fable-tailscale" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}"

COPY --from=build /out/tailwatch /out/tailwatch-agent /usr/local/bin/
# /data is pre-created owned by nonroot so named volumes inherit the ownership.
COPY --from=build --chown=65532:65532 /out/data /data

ENV TAILWATCH_DATA_DIR=/data \
    TAILWATCH_LOG_JSON=true
VOLUME ["/data"]
# The hub binds its Tailscale IP (see --listen); with --listen 0.0.0.0:8484 it
# would accept non-tailnet traffic, so prefer network_mode: host or a tailscale
# sidecar sharing the network namespace.
EXPOSE 8484
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/tailwatch"]
