# Development Environment

For complete development environment setup, use the root docker-compose.yml files.

## Quick Start

### Core Development

```bash

# From project root

cd ../..
docker compose up -d

# Services: pgsquash + postgres-primary

# RAM: ~500MB

```

### With Multi-Version Testing

```bash

# From project root

cd ../..
docker compose -f docker-compose.yml -f docker-compose.testing.yml up -d

# Services: core + PostgreSQL 17, 15, 13

# RAM: ~1.5GB

```

### With Development Tools

```bash

# From project root

cd ../..
docker compose -f docker-compose.yml -f docker-compose.tools.yml up -d

# Services: core + pgAdmin + Filebrowser

# RAM: ~800MB

# Access pgAdmin: http://localhost:5050

# Access Filebrowser: http://localhost:8081

```

## Alternative: Simplified Dev Compose

This directory contains `full-stack.yml` - a simplified alternative to the root compose files:

```bash

# From this directory

docker compose -f full-stack.yml up -d

# Services: pgsquash + postgres + pgadmin (with profile)

# RAM: ~500MB

```

## Documentation

For complete Docker infrastructure documentation, see:

- **[/docker-compose.yml](../../docker-compose.yml)** - Core services (2)
- **[/docker-compose.testing.yml](../../docker-compose.testing.yml)** - Multi-version testing (+3)
- **[/docker-compose.tools.yml](../../docker-compose.tools.yml)** - Development tools (+2)
- **[/docker/README.md](../README.md)** - Complete Docker infrastructure guide
- **[/docs/internal/audits/DOCKER\_INFRASTRUCTURE\_CHANGES.md](../../docs/internal/audits/DOCKER_INFRASTRUCTURE_CHANGES.md)** - Infrastructure changes and migration guide

## Environment Variables

Copy `.env.example` from the project root:

```bash
cd ../..
cp .env.example .env
nano .env
```

## Usage Patterns

### Local Development

```bash

# Core services only

docker compose up -d

# With pgAdmin for database management

docker compose -f docker-compose.yml -f docker-compose.tools.yml up -d
```

### Testing

```bash

# Multi-version PostgreSQL testing

docker compose -f docker-compose.yml -f docker-compose.testing.yml up -d

# Run validation tests

docker compose exec pgsquash pgsquash validate /app/migrations/*.sql
```

### Full Stack

```bash

# All services (core + testing + tools)

docker compose -f docker-compose.yml -f docker-compose.testing.yml -f docker-compose.tools.yml up -d

# Services: 7 total

# - pgsquash

# - postgres-primary

# - postgres-17, postgres-15, postgres-13

# - pgAdmin

# - Filebrowser

```

## See Also

- **Engine (CLI)**: [../engine/README.md](../engine/README.md)
- **Validation**: [../validation/README.md](../validation/README.md)
- **API Server**: [../api-server/README.md](../api-server/README.md)
