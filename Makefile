BINARY_NAME := capysquash
MAIN_PACKAGE := ./cmd/capysquash
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GIT_COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.buildDate=$(BUILD_DATE) -X main.gitCommit=$(GIT_COMMIT)

# pg_query_go links libpg_query, so CGO stays on and a C toolchain is required.
export CGO_ENABLED = 1

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-18s %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the capysquash binary
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY_NAME) $(MAIN_PACKAGE)

.PHONY: fmt
fmt: ## Format source
	gofmt -s -w .

.PHONY: lint
lint: ## Lint (golangci-lint if available, else go vet)
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run --timeout=5m; \
	else \
		go vet ./...; \
	fi

.PHONY: test
test: ## Run tests with the race detector
	go test -race ./...

.PHONY: check
check: fmt lint test ## fmt + lint + test

.PHONY: release-snapshot
release-snapshot: ## Local GoReleaser snapshot build (needs the goreleaser-cross toolchains)
	goreleaser release --snapshot --clean

.PHONY: clean
clean: ## Remove build artifacts
	rm -f $(BINARY_NAME)
	rm -rf dist
