# =============================================================================
# pgsquash API Server Dockerfile
# =============================================================================
# Multi-stage build for pgsquash API server
# =============================================================================

# Build arguments
ARG GO_VERSION=1.25.3
ARG BUILD_VERSION=dev
ARG BUILD_DATE
ARG GIT_COMMIT

# Build stage - using Ubuntu 24.04 LTS
FROM ubuntu:noble AS builder

# Re-declare ARGs for this stage
ARG GO_VERSION
ARG BUILD_VERSION
ARG BUILD_DATE
ARG GIT_COMMIT

# Install Go with architecture-specific binary
RUN apt-get update && apt-get install -y --no-install-recommends \
    wget \
    ca-certificates \
    git \
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
    ca-certificates \
    tzdata \
    gcc \
    libc6-dev \
    && rm -rf /var/lib/apt/lists/*

# Set working directory - build from project root
WORKDIR /app

# Copy go modules from project root
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download && go mod verify

# Copy entire project (API server imports internal packages)
COPY cmd/ ./cmd/
COPY internal/ ./internal/

# Build the API server with version info
# NOTE: CGO_ENABLED=1 required for pg_query_go dependency
WORKDIR /app
RUN CGO_ENABLED=1 go build \
    -ldflags="-w -s \
    -X 'main.version=${BUILD_VERSION}' \
    -X 'main.buildDate=${BUILD_DATE}' \
    -X 'main.gitCommit=${GIT_COMMIT}'" \
    -o api-server ./cmd/api-server

# Production stage - Ubuntu for reliability
FROM ubuntu:noble AS runtime

# Re-declare build metadata for labels
ARG BUILD_VERSION
ARG BUILD_DATE
ARG GIT_COMMIT

# Install runtime dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user with high UID to avoid conflicts
RUN groupadd -g 10001 pgsquash && \
    useradd -u 10001 -g pgsquash -s /bin/bash -m pgsquash

# Create app directory
WORKDIR /app

# Copy binary from builder stage and set ownership in one step
COPY --from=builder --chown=10001:10001 /app/api-server .

# Add OCI labels
LABEL org.opencontainers.image.title="pgsquash API Server" \
      org.opencontainers.image.description="API server for pgsquash migration squasher" \
      org.opencontainers.image.version="${BUILD_VERSION}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.revision="${GIT_COMMIT}" \
      org.opencontainers.image.vendor="CAPYSQUASH" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.source="https://github.com/CAPYSQUASH/pgsquash-engine" \
      org.opencontainers.image.documentation="https://github.com/CAPYSQUASH/pgsquash-engine/blob/main/docker/api-server/README.md"

# Switch to non-root user
USER pgsquash

# Expose port
EXPOSE 8080

# Health check (using curl instead of wget)
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/health || exit 1

# Run the binary
CMD ["./api-server"]
