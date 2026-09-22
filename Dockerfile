# syntax=docker/dockerfile:1
# ═════════════════════════════════════════════════════════════════════════════
# capysquash — PostgreSQL migration squasher.
#
#   docker build -t capysquash .
#   docker run --rm -v "$PWD/migrations:/app/migrations:ro" capysquash analyze /app/migrations
#   docker compose run --rm capysquash squash /app/migrations
#
# CGO is mandatory (github.com/pganalyze/pg_query_go links libpg_query), so
# this image is NOT cross-compiled: no --platform=$BUILDPLATFORM here. Build
# per-architecture on a native builder, or accept QEMU emulation.
# ═════════════════════════════════════════════════════════════════════════════

# ── build-time knobs (override with --build-arg or compose build.args) ───────
ARG BUILD_IMAGE=golang:1.27.1-trixie
ARG RUNTIME_IMAGE=ubuntu:resolute

ARG APP_UID=10001
ARG APP_GID=10001

ARG BUILD_VERSION=dev
ARG BUILD_DATE=unknown
ARG GIT_COMMIT=unknown

# ─────────────────────────────────────────────────────────────────────────────
# base — Go toolchain plus the C toolchain pg_query_go needs
# ─────────────────────────────────────────────────────────────────────────────
FROM ${BUILD_IMAGE} AS base
ENV CGO_ENABLED=1 GOFLAGS=-mod=readonly GOTOOLCHAIN=local
WORKDIR /src

# ─────────────────────────────────────────────────────────────────────────────
# deps — module download + verify, warmed into a persistent builder cache
# ─────────────────────────────────────────────────────────────────────────────
FROM base AS deps
RUN --mount=type=bind,source=.,target=.,ro \
    --mount=type=cache,target=/go/pkg/mod,id=gomod \
    go mod download && go mod verify

# ─────────────────────────────────────────────────────────────────────────────
# build — compile the binary into /out
# ─────────────────────────────────────────────────────────────────────────────
FROM deps AS build
ARG TARGETARCH
ARG BUILD_VERSION BUILD_DATE GIT_COMMIT
RUN --mount=type=bind,source=.,target=.,ro \
    --mount=type=cache,target=/go/pkg/mod,id=gomod \
    --mount=type=cache,target=/root/.cache/go-build,id=gobuild-capysquash-${TARGETARCH} \
    mkdir -p /out/bin && \
    go build -trimpath \
      -ldflags="-s -w \
        -X 'main.version=${BUILD_VERSION}' \
        -X 'main.buildDate=${BUILD_DATE}' \
        -X 'main.gitCommit=${GIT_COMMIT}'" \
      -o /out/bin/capysquash ./cmd/capysquash

# ─────────────────────────────────────────────────────────────────────────────
# runtime — default target
#   Not distroless: Docker-based validation shells out to the docker CLI and
#   psql, and --branch-check needs git.
# ─────────────────────────────────────────────────────────────────────────────
FROM ${RUNTIME_IMAGE} AS runtime
ARG APP_UID APP_GID BUILD_VERSION BUILD_DATE GIT_COMMIT

LABEL org.opencontainers.image.title="capysquash" \
      org.opencontainers.image.description="PostgreSQL migration squasher and optimizer" \
      org.opencontainers.image.version="${BUILD_VERSION}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.revision="${GIT_COMMIT}" \
      org.opencontainers.image.vendor="CapyDB" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.source="https://github.com/capydatabase/capysquash" \
      org.opencontainers.image.documentation="https://github.com/capydatabase/capysquash/blob/main/README.md"

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    tzdata \
    postgresql-client \
    docker.io \
    git \
  && rm -rf /var/lib/apt/lists/* \
  && groupadd -g ${APP_GID} capysquash \
  && useradd -u ${APP_UID} -g capysquash -s /bin/bash -m -d /home/capysquash capysquash \
  && mkdir -p /app/migrations /app/output /app/config \
  && chown -R ${APP_UID}:${APP_GID} /app

COPY --from=build --chown=${APP_UID}:${APP_GID} /out/bin/capysquash /usr/local/bin/capysquash

USER ${APP_UID}:${APP_GID}
WORKDIR /app

ENTRYPOINT ["capysquash"]
CMD ["--help"]
