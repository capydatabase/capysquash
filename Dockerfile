# =============================================================================
# pgsquash-engine Dockerfile
# ======================================================================

# Build arguments
ARG GO_VERSION=1.26.5
ARG BUILD_VERSION=dev
ARG BUILD_DATE
ARG GIT_COMMIT

# Build stage

# Alternative: golang:1.26.4 (Debian) works fine for building
FROM ubuntu:resolute AS builder

# Re-declare ARGs for this stage
ARG GO_VERSION
ARG BUILD_VERSION
ARG BUILD_DATE
ARG GIT_COMMIT
ARG TARGETARCH

# Install Go with architecture-specific binary
RUN apt-get update && apt-get install -y --no-install-recommends \
    wget \
    ca-certificates \
    && ARCH=$(dpkg --print-architecture) \
    && wget https://go.dev/dl/go${GO_VERSION}.linux-${ARCH}.tar.gz \
    && tar -C /usr/local -xzf go${GO_VERSION}.linux-${ARCH}.tar.gz \
    && rm go${GO_VERSION}.linux-${ARCH}.tar.gz \
    && rm -rf /var/lib/apt/lists/*

ENV PATH="/usr/local/go/bin:${PATH}"
ENV GOPATH="/go"
ENV PATH="${GOPATH}/bin:${PATH}"

# Install build dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    ca-certificates \
    tzdata \
    gcc \
    libc6-dev \
    && rm -rf /var/lib/apt/lists/*

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Build the application with version information
RUN CGO_ENABLED=1 go build \
    -ldflags="-w -s \
    -X 'main.version=${BUILD_VERSION}' \
    -X 'main.buildDate=${BUILD_DATE}' \
    -X 'main.gitCommit=${GIT_COMMIT}'" \
    -o pgsquash ./cmd/pgsquash

# Runtime stage
FROM ubuntu:resolute AS runtime

# Re-declare build metadata for labels
ARG BUILD_VERSION
ARG BUILD_DATE
ARG GIT_COMMIT

# Install runtime dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    tzdata \
    postgresql-client \
    docker.io \
    bash \
    curl \
    jq \
    git \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user (Ubuntu syntax) - use non-conflicting UID
RUN groupadd -r pgsquash && \
    useradd -r -g pgsquash -s /bin/bash -m pgsquash

# Create necessary directories
RUN mkdir -p /app/migrations /app/output /app/config /app/logs && \
    chown -R pgsquash:pgsquash /app

# Copy binary from builder
COPY --from=builder /app/pgsquash /usr/local/bin/pgsquash

# Copy configuration templates and scripts
COPY docker/init-scripts/ /app/scripts/
COPY docker/config-templates/ /app/templates/
COPY docker/entrypoint.sh /app/entrypoint.sh

# Make scripts executable
RUN chmod +x /app/entrypoint.sh /app/scripts/*.sh

# Set user
USER pgsquash

# Set working directory
WORKDIR /app

# Add OCI labels
LABEL org.opencontainers.image.title="pgsquash-engine" \
      org.opencontainers.image.description="PostgreSQL migration squasher and optimizer" \
      org.opencontainers.image.version="${BUILD_VERSION}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.revision="${GIT_COMMIT}" \
      org.opencontainers.image.vendor="CAPYSQUASH" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.source="https://github.com/CAPYSQUASH/pgsquash-engine" \
      org.opencontainers.image.documentation="https://github.com/CAPYSQUASH/pgsquash-engine/blob/main/README.md"

# Health check - using dedicated health endpoint
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD pgsquash health || exit 1

# Expose ports (for web UI and API)
EXPOSE 8080

# Default command
ENTRYPOINT ["/app/entrypoint.sh"]
CMD ["--help"]