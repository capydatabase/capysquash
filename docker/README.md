# Docker Infrastructure

Complete Docker setup for pgsquash with modular compose files for different use cases.

## Quick Start

### Core Development (2 services)

```bash

# Start essential services only

docker compose up -d

# Services: pgsquash + postgres-primary (PostgreSQL 17)

# RAM usage: ~500MB

```

### Multi-Version Testing (5 services)

```bash

# Add PostgreSQL 17, 16, 15 for version compatibility testing

docker compose -f docker-compose.yml -f docker-compose.testing.yml up -d

# Services: core + postgres-17 + postgres-16 + postgres-15

# RAM usage: ~1.5GB

```

### Development Tools (4 services)

```bash

# Add pgAdmin and Filebrowser for GUI management

docker compose -f docker-compose.yml -f docker-compose.tools.yml up -d

# Services: core + pgAdmin + Filebrowser

# RAM usage: ~800MB

# Access pgAdmin: http://localhost:5050

# Access Filebrowser: http://localhost:8081

```

### API Server (separate repository)

> **Note**: The API server has been moved to a separate repository: [capysquash-api](https://github.com/CAPYSQUASH/capysquash-api)

```bash

# Clone and deploy the API server (separate repository)

git clone https://github.com/CAPYSQUASH/capysquash-api
cd capysquash-api
docker compose up -d

# Service: API server only

# RAM usage: ~200MB

# Access API: http://localhost:8080/health

```

See [docker/api-server/README.md](api-server/README.md) in this repository for migration information.

---

## Directory Structure

```
/Dockerfile                       # ← Main application Dockerfile

/docker-compose.yml               # ← Core services (2)

/docker-compose.testing.yml       # ← Multi-version PostgreSQL testing

/docker-compose.tools.yml         # ← Optional development tools

docker/
├── README.md                     # This file

├── entrypoint.sh                # Container entrypoint script

│
├── api-server/                  # API Server Migration Guide (see README)

│   └── README.md                # ⚠️ API moved to capysquash-api repository

│
├── engine/                      # Containerized CLI

│   ├── quick-start.yml
│   ├── examples.sh
│   └── README.md
│
├── validation/                  # Validation Tools

│   ├── init-scripts/
│   └── README.md
│
├── init-scripts/                # PostgreSQL Initialization

│   ├── supabase-compat.sql
│   ├── init-postgres.sql
│   └── validation-init.sql
│
├── scripts/                     # Helper Scripts

│   ├── build.sh                # Build Docker images

│   ├── cleanup.sh              # Cleanup containers

│   ├── multi-version-test.sh   # Multi-version testing

│   └── test-setup.sh           # Test setup

│
└── dev-environment/             # Development Environment

    ├── full-stack.yml
    └── README.md
```

---

## Deployment Scenarios

### 1. Containerized CLI

Run pgsquash CLI in a container without installing Go.

**Use Case**: CI/CD pipelines, team consistency, no Go installation required

```bash

# Squash migrations

docker compose -f docker/engine/quick-start.yml run --rm pgsquash squash /app/migrations/*.sql

# Analyze migrations

docker compose -f docker/engine/quick-start.yml run --rm pgsquash analyze /app/migrations/*.sql

# Interactive shell

docker compose -f docker/engine/quick-start.yml run --rm pgsquash bash
```

**Documentation**: [engine/README.md](engine/README.md)

---

### 2. Validation with Docker

Spin up ephemeral PostgreSQL containers to validate migration squashing results.

**Use Case**: Automated testing, schema validation, multi-version compatibility

```bash

# Quick validation (recommended)

./docker/scripts/quick-validate.sh

# Multi-version testing (PostgreSQL 17, 15, 13)

./docker/scripts/multi-version-test.sh

# Cleanup validation containers

./docker/scripts/cleanup.sh
```

**Documentation**: [validation/README.md](validation/README.md)

---

### 3. API Server (Moved to Separate Repository)

> **⚠️ Important**: The API server has been moved to its own repository for better modularity.

**New Location**: [capysquash-api](https://github.com/CAPYSQUASH/capysquash-api)

HTTP API with GitHub webhook integration for platforms.

**Use Case**: Web frontends, mobile apps, GitHub PR automation

```bash

# Clone the separate repository

git clone https://github.com/CAPYSQUASH/capysquash-api
cd capysquash-api
docker compose up -d

# Test health endpoint

curl http://localhost:8080/health
```

**API Endpoints**:

- `GET /health` - Health check
- `POST /api/analyze` - Analyze migrations
- `POST /api/squash` - Squash migrations
- `POST /github/webhook` - GitHub webhook handler
- `GET /github/login` - GitHub OAuth login

**Documentation**:

- **Migration Guide**: [docker/api-server/README.md](api-server/README.md) (in this repo)
- **Full Docs**: [capysquash-api repository](https://github.com/CAPYSQUASH/capysquash-api)

---

### 4. Development Environment

Complete development environment with core services.

**Use Case**: Local development, debugging, full workflow testing

```bash

# Core services (2)

docker compose up -d

# With multi-version testing (5)

docker compose -f docker-compose.yml -f docker-compose.testing.yml up -d

# With development tools (4)

docker compose -f docker-compose.yml -f docker-compose.tools.yml up -d

# Everything together (7)

docker compose -f docker-compose.yml -f docker-compose.testing.yml -f docker-compose.tools.yml up -d
```

**Documentation**: [dev-environment/README.md](dev-environment/README.md)

---

## Platform

The pgsquash Platform (Next.js frontend + API backend) is maintained in a **separate repository**:

**Repository**: `/capysquash-platform/` (separate from engine)

For Platform deployment and documentation, see the Platform repository.

---

## Modular Compose Files

### Root Compose Files

| File                         | Services | Purpose                                     | RAM Usage |
| ---------------------------- | -------- | ------------------------------------------- | --------- |
| `docker-compose.yml`         | 2        | Core services (pgsquash + postgres-primary) | \~500MB   |
| `docker-compose.testing.yml` | +3       | Add PostgreSQL 17, 15, 13 for testing       | +1GB      |
| `docker-compose.tools.yml`   | +2       | Add pgAdmin + Filebrowser                   | +300MB    |

### Subdirectory Compose Files

| File                             | Services | Purpose              | RAM Usage |
| -------------------------------- | -------- | -------------------- | --------- |
| `engine/quick-start.yml`         | 1        | CLI-only container   | \~200MB   |
| `dev-environment/full-stack.yml` | 2        | Simplified dev setup | \~500MB   |

> **Note**: API server compose files have moved to [capysquash-api repository](https://github.com/CAPYSQUASH/capysquash-api)

---

## Helper Scripts

All scripts located in `docker/scripts/`:

### Build & Publish

**`build.sh`** - Build and publish Docker images

```bash

# Build locally

./docker/scripts/build.sh

# Build and push to registry

./docker/scripts/build.sh --registry docker.io --repository myuser/pgsquash --push

# Multi-Platform build

./docker/scripts/build.sh --Platforms "linux/amd64,linux/arm64" --push

# Build specific version

./docker/scripts/build.sh --version v1.2.3 --push
```

**Features**:

- Multi-architecture builds (amd64, arm64)
- Automatic version tagging from git
- Build cache optimization
- Security scanning with Trivy (if installed)

---

### Validation & Testing

**`multi-version-test.sh`** - Test against multiple PostgreSQL versions

```bash

# Test all default versions (17, 15, 13)

./docker/scripts/multi-version-test.sh

# Test specific versions

./docker/scripts/multi-version-test.sh --versions 17,15,13

# Quick test (latest and oldest only)

./docker/scripts/multi-version-test.sh --quick
```

**Features**:

- Parallel version testing
- Detailed logs per version
- Schema comparison across versions
- Summary report generation

---

**`cleanup.sh`** - Clean up Docker resources

```bash

# Clean validation containers only (default)

./docker/scripts/cleanup.sh

# Clean everything (containers, images, volumes)

./docker/scripts/cleanup.sh --all --volumes

# Skip confirmation prompts

./docker/scripts/cleanup.sh --all --force
```

**Features**:

- Label-based container cleanup
- Volume management
- Compose service cleanup
- Safety confirmations

---

**`test-setup.sh`** - Comprehensive Docker setup testing

```bash

# Run all tests

./docker/scripts/test-setup.sh
```

**Tests**:

- Docker availability
- Image building
- Container runtime
- Validation workflow
- Full end-to-end workflow
- Multi-Platform support

---

## Configuration

### Environment Variables

Copy template and customize:

```bash
cp .env.example .env
nano .env
```

**Core Variables**:

```bash
POSTGRES_PASSWORD=pgsquash_secure_password
POSTGRES_PRIMARY_PORT=5432
```

**API Server Variables** (for [capysquash-api](https://github.com/CAPYSQUASH/capysquash-api)):

> See [capysquash-api environment variables documentation](https://github.com/CAPYSQUASH/capysquash-api/blob/main/docs/ENVIRONMENT_VARIABLES.md) for complete reference.

```bash
GITHUB_TOKEN=ghp_your_token_here
GITHUB_WEBHOOK_SECRET=your_webhook_secret
GITHUB_CLIENT_ID=your_oauth_client_id
GITHUB_CLIENT_SECRET=your_oauth_client_secret
```

**Testing Variables**:

```bash
POSTGRES_17_PORT=5417
POSTGRES_15_PORT=5415
POSTGRES_13_PORT=5413
```

**Tool Variables**:

```bash
PGADMIN_PORT=5050
FILEBROWSER_PORT=8081
```

### Config File

```bash
cp pgsquash.config.example.json pgsquash.config.json
nano pgsquash.config.json
```

---

## Migration from Old Structure

If you used the old Docker setup (11 services), see migration guide:

**[DOCKER_INFRASTRUCTURE_CHANGES.md](../docs/internal/audits/DOCKER_INFRASTRUCTURE_CHANGES.md)**

**What Changed**:

- 11 services → 2 core services (pgsquash + postgres-primary)
- Removed services: Redis, MinIO, Grafana, Prometheus, Traefik, pgAdmin, Filebrowser
- Moved testing: postgres-17, postgres-16, postgres-15 → `docker-compose.testing.yml`
- Moved tools: pgAdmin, Filebrowser → `docker-compose.tools.yml`

**Impact**:

- ✅ 75% faster startup (15s vs 60s)
- ✅ 75% less RAM usage (500MB vs 2GB)
- ✅ Modular compose files for different use cases
- ✅ Clear service integration status

---

## Best Practices

### Development

```bash

# Start core services for basic development

docker compose up -d

# Add tools when needed

docker compose -f docker-compose.yml -f docker-compose.tools.yml up -d
```

### CI/CD

```bash

# Use engine for CI/CD pipelines

docker compose -f docker/engine/quick-start.yml run --rm pgsquash squash

# Use multi-version testing for compatibility

./docker/scripts/multi-version-test.sh --quick
```

### Production

```bash

# Use API server for production deployments (separate repository)

git clone https://github.com/CAPYSQUASH/capysquash-api
cd capysquash-api
docker compose up -d

# Set resource limits in compose file

deploy:
  resources:
    limits:
      cpus: '2.0'
      memory: 2G
```

See [capysquash-api deployment guide](https://github.com/CAPYSQUASH/capysquash-api) for production setup.

---

## Troubleshooting

### Build Issues

```bash

# Clean build

docker compose build --no-cache

# Check Dockerfile syntax

docker build --dry-run -f Dockerfile .
```

### Validation Issues

```bash

# Check Docker socket

docker info

# List validation containers

docker ps -a --filter "label=pgsquash.type=validation"

# Clean up stuck containers

./docker/scripts/cleanup.sh
```

### Permission Issues

```bash

# Fix ownership

sudo chown -R $(id -u):$(id -g) output/

# Check user in container

docker compose run --rm pgsquash id
```

### Port Conflicts

```bash

# Check what's using the port

lsof -i :5432

# Or change the port in .env

echo "POSTGRES_PRIMARY_PORT=5433" >> .env
```

---

## Advanced

### Custom PostgreSQL Image

Create custom Postgres image with additional extensions:

```dockerfile

# docker/validation/custom-postgres.dockerfile

FROM postgres:17

RUN apt-get update && apt-get install -y --no-install-recommends \
    postgresql-17-postgis-3 \
    postgresql-17-pgvector \
    && rm -rf /var/lib/apt/lists/*
```

Build and use:

```bash
docker build -t pgsquash-postgres:custom -f docker/validation/custom-postgres.dockerfile .

# Update compose file

postgresql_image: pgsquash-postgres:custom
```

### Multi-Architecture Build

```bash
docker buildx create --use
docker buildx build --Platform linux/amd64,linux/arm64 -t pgsquash:latest .
```

---

## Documentation

- **API Server**: [api-server/README.md](api-server/README.md) (migration guide) | [capysquash-api repository](https://github.com/CAPYSQUASH/capysquash-api) (current docs)
- **CLI Engine**: [engine/README.md](engine/README.md)
- **Validation**: [validation/README.md](validation/README.md)
- **Development**: [dev-environment/README.md](dev-environment/README.md)
- **Infrastructure Changes**: [../docs/internal/audits/DOCKER_INFRASTRUCTURE_CHANGES.md](../docs/internal/audits/DOCKER_INFRASTRUCTURE_CHANGES.md)

---

## Support

Issues? Check:

1. [Troubleshooting Guide](../Docs/troubleshooting.md)
2. [GitHub Issues](https://github.com/capysquash/pgsquash-engine/issues)
3. Container logs: `docker compose logs -f`
4. [Infrastructure Changes Guide](../Docs/internal/audits/DOCKER_INFRASTRUCTURE_CHANGES.md)
