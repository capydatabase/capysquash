#!/bin/bash
# pgsquash Docker Run Examples with Best Practices
# This file provides example docker run commands with proper resource limits and security settings

set -euo pipefail

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo_example() {
    echo -e "${BLUE}# $1${NC}"
    echo -e "${GREEN}$2${NC}"
    echo ""
}

# =============================================================================
# BASIC USAGE
# =============================================================================

echo_example "Basic usage - squash migrations" \
'docker run --rm \
  --memory="512m" \
  --cpus="1.0" \
  --pids-limit=100 \
  -v $(pwd)/migrations:/app/migrations:ro \
  -v $(pwd)/output:/app/output \
  pgsquash:latest squash'

# =============================================================================
# WITH VALIDATION (requires Docker socket)
# =============================================================================

echo_example "With Docker validation" \
'docker run --rm \
  --memory="1g" \
  --cpus="2.0" \
  --pids-limit=200 \
  -v $(pwd)/migrations:/app/migrations:ro \
  -v $(pwd)/output:/app/output \
  -v /var/run/docker.sock:/var/run/docker.sock \
  pgsquash:latest workflow --validate'

# =============================================================================
# SECURE PRODUCTION SETUP
# =============================================================================

echo_example "Production with enhanced security" \
'docker run --rm \
  --memory="512m" \
  --cpus="1.0" \
  --pids-limit=100 \
  --read-only \
  --tmpfs /tmp:rw,noexec,nosuid,size=65536k \
  --cap-drop=ALL \
  --cap-add=NET_BIND_SERVICE \
  --security-opt=no-new-privileges \
  -v $(pwd)/migrations:/app/migrations:ro \
  -v $(pwd)/output:/app/output \
  -v $(pwd)/config:/app/config:ro \
  pgsquash:latest squash'

# =============================================================================
# CUSTOM CONFIGURATION
# =============================================================================

echo_example "With custom configuration" \
'docker run --rm \
  --memory="512m" \
  --cpus="1.0" \
  -e PGSQUASH_SAFETY_LEVEL=aggressive \
  -e PGSQUASH_AUTO_VALIDATE=false \
  -e PGSQUASH_LOG_LEVEL=debug \
  -v $(pwd)/migrations:/app/migrations:ro \
  -v $(pwd)/output:/app/output \
  -v $(pwd)/pgsquash.config.json:/app/config/pgsquash.config.json:ro \
  pgsquash:latest squash'

# =============================================================================
# INITIALIZE WITH SAMPLE DATA
# =============================================================================

echo_example "Initialize with sample migrations" \
'docker run --rm \
  --memory="512m" \
  --cpus="1.0" \
  -e PGSQUASH_INIT_SAMPLE=true \
  -v $(pwd):/app/migrations \
  pgsquash:latest init'

# =============================================================================
# COMPLETE WORKFLOW
# =============================================================================

echo_example "Complete workflow: analyze → squash → validate" \
'docker run --rm \
  --memory="1g" \
  --cpus="2.0" \
  --pids-limit=200 \
  -v $(pwd)/migrations:/app/migrations:ro \
  -v $(pwd)/output:/app/output \
  -v /var/run/docker.sock:/var/run/docker.sock \
  pgsquash:latest workflow \
    --input /app/migrations \
    --output /app/output \
    --safety-level standard \
    --validate \
    --docker \
    --progress'

# =============================================================================
# DOCKER COMPOSE USAGE
# =============================================================================

echo_example "Using docker-compose (basic)" \
'# In docker-compose.yml:
services:
  pgsquash:
    image: pgsquash:latest
    volumes:
      - ./migrations:/app/migrations:ro
      - ./output:/app/output
    deploy:
      resources:
        limits:
          cpus: "1.0"
          memory: 512M
        reservations:
          cpus: "0.5"
          memory: 256M

# Then run:
docker-compose run --rm pgsquash squash'

# =============================================================================
# RESOURCE LIMIT EXPLANATIONS
# =============================================================================

cat << 'EOF'

=============================================================================
RESOURCE LIMIT RECOMMENDATIONS
=============================================================================

Memory Limits:
  --memory="512m"   - Standard squashing (< 150 migrations)
  --memory="1g"     - Large squashing (150-400 migrations)
  --memory="2g"     - Very large or with validation

CPU Limits:
  --cpus="1.0"      - Standard usage
  --cpus="2.0"      - With validation or parallel processing
  --cpus="4.0"      - High-performance scenarios

Process Limits:
  --pids-limit=100  - Standard (prevents fork bombs)
  --pids-limit=200  - With validation (multiple containers)

Security Options:
  --read-only              - Read-only root filesystem
  --tmpfs /tmp             - Writable temporary storage
  --cap-drop=ALL           - Drop all capabilities
  --cap-add=...            - Add only needed capabilities
  --security-opt=no-new-privileges  - Prevent privilege escalation

=============================================================================
TROUBLESHOOTING
=============================================================================

If you encounter "out of memory" errors:
  - Increase --memory limit
  - Enable streaming mode for large datasets
  - Process migrations in smaller batches

If containers fail to start during validation:
  - Increase --pids-limit
  - Check Docker daemon has enough resources
  - Ensure port range 15432-16432 is available

If permission errors occur:
  - Ensure output directory is writable
  - Check user/group ownership (1000:1000)
  - Remove --read-only if necessary

=============================================================================

EOF
