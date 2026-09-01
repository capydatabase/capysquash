# Validation: Docker Validation Containers

Spin up ephemeral PostgreSQL containers to validate migration squashing results using Docker-in-Docker.

## Purpose

- Automatically create validation databases
- Compare original vs squashed migrations
- Test against multiple PostgreSQL versions
- Ensure schema integrity

## How It Works

1. pgsquash runs with Docker socket access
2. Creates two PostgreSQL containers on-demand
3. Applies original migrations to container 1
4. Applies squashed migration to container 2
5. Compares schemas and reports differences
6. Cleans up containers automatically

## Quick Start

### Simple Validation

```bash

# Validate migrations in current directory

./docker/scripts/quick-validate.sh
```

### With Docker Compose

```bash

# Full validation workflow

docker compose -f docker/validation/with-validation.yml run --rm pgsquash validate /app/migrations/*.sql

# Squash + validate

docker compose -f docker/validation/with-validation.yml run --rm pgsquash workflow
```

### Multi-Version Testing

```bash

# Test against PostgreSQL 17, 16, 15, 14, 13

./docker/scripts/multi-version-test.sh

# Quick test (latest and oldest)

./docker/scripts/multi-version-test.sh --quick
```

## Helper Scripts

Located in `docker/scripts/`:

### `quick-validate.sh` - Fastest validation

```bash
./docker/scripts/quick-validate.sh my-migrations/
```

### `validate.sh` - Comprehensive validation

```bash

# Full validation with detailed reports

./docker/scripts/validate.sh

# Keep containers for inspection

./docker/scripts/validate.sh --keep-container
```

### `setup-validation.sh` - Custom validation environment

```bash

# Setup validation with auto-detected extensions

./docker/scripts/setup-validation.sh migrations/ /tmp/validation

# Run the generated validation

cd /tmp/validation
./run-validation.sh
```

### `multi-version-test.sh` - Test multiple PostgreSQL versions

```bash

# All versions

./docker/scripts/multi-version-test.sh

# Specific versions

./docker/scripts/multi-version-test.sh --versions 17,15,13
```

### `cleanup.sh` - Clean up validation containers

```bash

# Clean validation containers only

./docker/scripts/cleanup.sh

# Clean everything

./docker/scripts/cleanup.sh --all --volumes
```

## Configuration

### ValidationConfig (in pgsquash.config.json)

```json
{
  "validation": {
    "enabled": true,
    "docker_enabled": true,
    "postgresql_version": "17",
    "container_ready_timeout_seconds": 30,
    "max_port_search_attempts": 1000,
    "validation_approach": "two_containers"
  }
}
```

### Environment Variables

```bash
export PGSQUASH_DOCKER_ENABLED=true
export PGSQUASH_AUTO_VALIDATE=true
export POSTGRES_VERSION=17  # Default validation version

```

## Validation Approaches

### 1. Two Containers (Default)

- Spins up 2 PostgreSQL containers
- Container 1: Original migrations
- Container 2: Squashed migration
- Compares schemas via SQL queries

### 2. Two Databases

- Single PostgreSQL container
- Database 1: Original migrations
- Database 2: Squashed migration
- Faster but less isolated

### 3. Schema Diff

- Generates SQL schema dumps
- Uses diff tool for comparison
- Useful for detailed analysis

## Docker Socket Access

**Important**: This use case requires Docker socket access for creating validation containers.

Security considerations:

- Only grant to trusted environments
- Use Unix socket (`/var/run/docker.sock`)
- Consider using [Docker socket proxy](https://github.com/Tecnativa/docker-socket-proxy)

## Validation Resources

### `init-scripts/`

- `supabase-compat.sql` - Supabase auth compatibility
- `init-postgres.sql` - Basic PostgreSQL setup
- `validation-init.sql` - Validation-specific setup

### `validation/`

- Validation-specific Dockerfile (optimized)
- Custom PostgreSQL images with extensions
- Validation helpers

## Container Lifecycle

1. **Creation**: Containers created with labels (`pgsquash.type=validation`)
2. **Execution**: Migrations applied, schemas compared
3. **Cleanup**: Automatic cleanup via labels (no AutoRemove)
4. **Inspection**: Use ` - keep-container` to inspect manually

## Troubleshooting

### Port Conflicts

If you see “port already in use”:

```bash

# Check running validation containers

docker ps -a -f "label=pgsquash.type=validation"

# Clean up

./docker/scripts/cleanup.sh
```

### Extension Installation Failures

If extensions fail to install:

```bash

# Check available extensions

docker run --rm postgres:17 psql -U postgres -c "SELECT name FROM pg_available_extensions ORDER BY name;"

# Create custom image with extensions

# See validation/custom-postgres.dockerfile

```

### Container Not Ready

If timeout occurs:

```json
{
  "validation": {
    "container_ready_timeout_seconds": 60
  }
}
```

### Schema Comparison Differences

Check detailed logs:

```bash

# Use comprehensive validation

./docker/scripts/validate.sh --keep-container

# Inspect databases

docker exec -it pgsquash-validation-postgres psql -U postgres -d original_migrations
docker exec -it pgsquash-validation-postgres psql -U postgres -d squashed_migrations
```

## Performance

- **Container startup**: \~2-5 seconds
- **Migration application**: Varies by size
- **Schema comparison**: <1 second
- **Total validation**: \~10-30 seconds typical

## Use Cases

✅ **Good for**:

- CI/CD validation pipelines
- Pre-production verification
- Multi-version compatibility testing
- Regression testing

❌ **Not suitable for**:

- Production deployments
- Environments without Docker
- Restricted Docker socket access

## Security

### Container Isolation

Validation containers are:

- ✅ Isolated in separate network
- ✅ Labeled for tracking
- ✅ Resource limited (512MB memory, 1 CPU)
- ✅ Automatically cleaned up
- ✅ Read-only where possible

### Best Practices

1. Use Docker socket proxy in production
2. Limit resource usage
3. Clean up regularly
4. Use session-based tracking
5. Monitor container creation

## Next Steps

- **For simple CLI**: See [Engine](../engine)
- **For API server**: See [API Server](../api-server)
- **For development**: See [dev-environment](../dev-environment)
