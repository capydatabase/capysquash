# syntax=docker/dockerfile:1
# ═════════════════════════════════════════════════════════════════════════════
# pgsquash-engine — PostgreSQL migration squasher.
#
#   docker build .                  → runtime image (default target)
#   docker compose up -d            → engine + a Postgres to validate against
#
# CGO is mandatory (github.com/pganalyze/pg_query_go links libpg_query), so
# this image is NOT cross-compiled: no --platform=$BUILDPLATFORM here. Build
# per-architecture on a native builder, or accept QEMU emulation.
# ═════════════════════════════════════════════════════════════════════════════

# ── build-time knobs (override with --build-arg or compose build.args) ───────
ARG BUILD_IMAGE=golang:1.27.1-trixie
ARG RUNTIME_IMAGE=ubuntu:resolute

ARG APP_PORT=8080
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
# build — compile and stage everything that ships into /out
# ─────────────────────────────────────────────────────────────────────────────
FROM deps AS build
ARG TARGETARCH
ARG BUILD_VERSION BUILD_DATE GIT_COMMIT
RUN --mount=type=bind,source=.,target=.,ro \
    --mount=type=cache,target=/go/pkg/mod,id=gomod \
    --mount=type=cache,target=/root/.cache/go-build,id=gobuild-pgsquash-${TARGETARCH} \
    mkdir -p /out/bin /out/app && \
    go build -trimpath \
      -ldflags="-s -w \
        -X 'main.version=${BUILD_VERSION}' \
        -X 'main.buildDate=${BUILD_DATE}' \
        -X 'main.gitCommit=${GIT_COMMIT}'" \
      -o /out/bin/pgsquash ./cmd/pgsquash && \
    cp -r docker/init-scripts /out/app/scripts && \
    cp -r docker/config-templates /out/app/templates && \
    cp docker/entrypoint.sh /out/app/entrypoint.sh && \
    chmod +x /out/app/entrypoint.sh /out/app/scripts/*.sh

# ─────────────────────────────────────────────────────────────────────────────
# runtime — default target
#   Not distroless: the entrypoint is bash, and the validation features shell
#   out to psql, git and the docker CLI.
# ─────────────────────────────────────────────────────────────────────────────
FROM ${RUNTIME_IMAGE} AS runtime
ARG APP_PORT APP_UID APP_GID BUILD_VERSION BUILD_DATE GIT_COMMIT

LABEL org.opencontainers.image.title="pgsquash-engine" \
      org.opencontainers.image.description="PostgreSQL migration squasher and optimizer" \
      org.opencontainers.image.version="${BUILD_VERSION}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.revision="${GIT_COMMIT}" \
      org.opencontainers.image.vendor="CAPYSQUASH" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.source="https://github.com/capysquash/pgsquash-engine" \
      org.opencontainers.image.documentation="https://github.com/capysquash/pgsquash-engine/blob/main/README.md"

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    tzdata \
    postgresql-client \
    docker.io \
    bash \
    curl \
    jq \
    git \
  && rm -rf /var/lib/apt/lists/* \
  && groupadd -g ${APP_GID} pgsquash \
  && useradd -u ${APP_UID} -g pgsquash -s /bin/bash -m -d /home/pgsquash pgsquash \
  && mkdir -p /app/migrations /app/output /app/config /app/logs \
  && chown -R ${APP_UID}:${APP_GID} /app

COPY --from=build --chown=${APP_UID}:${APP_GID} /out/bin/ /usr/local/bin/
COPY --from=build --chown=${APP_UID}:${APP_GID} /out/app/ /app/

USER ${APP_UID}:${APP_GID}
WORKDIR /app
EXPOSE ${APP_PORT}

# Healthcheck lives in compose so it can be tuned per environment.
ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["--help"]
