#!/bin/bash
set -e

echo "Building pgsquash..."

# Check if Go is available
if ! command -v go &> /dev/null; then
    echo "Go is not installed or not in PATH"
    exit 1
fi

# Get version from git if available, otherwise use "dev"
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(date -u '+%Y-%m-%d_%H:%M:%S')
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")

# Build flags
LDFLAGS="-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -X main.Commit=${COMMIT}"

# Build the binary
go build -ldflags "${LDFLAGS}" -o pgsquash cmd/pgsquash/main.go

echo "☑ Build completed: pgsquash"
echo "Version: ${VERSION}"
echo "Build time: ${BUILD_TIME}"
echo "Commit: ${COMMIT}"