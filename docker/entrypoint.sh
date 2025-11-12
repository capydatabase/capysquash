#!/bin/bash

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# Source common library if available, otherwise use inline definitions
if [ -f "$SCRIPT_DIR/../scripts/lib/common.sh" ]; then
  # shellcheck disable=SC1091
  source "$SCRIPT_DIR/../scripts/lib/common.sh"
else
  # Fallback inline definitions for Docker container
  RED='\033[0;31m'
  GREEN='\033[0;32m'
  YELLOW='\033[1;33m'
  BLUE='\033[0;34m'
  PURPLE='\033[0;35m'
  CYAN='\033[0;36m'
  NC='\033[0m'

  log_info() { echo -e "${CYAN}[INFO]${NC} $1" >&2; }
  log_success() { echo -e "${GREEN}[SUCCESS]${NC} $1" >&2; }
  log_warning() { echo -e "${YELLOW}[WARNING]${NC} $1" >&2; }
  log_error() { echo -e "${RED}[ERROR]${NC} $1" >&2; }
  log_step() { echo -e "${PURPLE}[STEP]${NC} $1" >&2; }
  setup_error_handling() { set -euo pipefail; }
  show_pgsquash_banner() {
    cat << 'EOF'
    ____  ____      ____   ____  __  __  ____  _____ __  __
   |  _ \/ ___|    / ___| / ___||  \/  |/ _  \/ ___||  \/  |
   | |_) \___ \   \___ \| |    | |\/| |  | | \___ \| |\/| |
   |  __/ ___) |   ___) | |___ | |  | |  |_| |___) | |  | |
   |_|   |____/   |____/ \____||_|  |_|\____|____/|_|  |_|

   🚀 PostgreSQL Migration Squasher - Fully Automated Setup

EOF
  }
fi

setup_error_handling

show_banner() {
  show_pgsquash_banner
}

# Environment setup
setup_environment() {
    log_step "Setting up environment..."

    # Set default values
    export PGSQUASH_HOME=${PGSQUASH_HOME:-/app}
    export PGSQUASH_CONFIG=${PGSQUASH_CONFIG:-$PGSQUASH_HOME/config/pgsquash.config.json}
    export PGSQUASH_MIGRATIONS_DIR=${PGSQUASH_MIGRATIONS_DIR:-$PGSQUASH_HOME/migrations}
    export PGSQUASH_OUTPUT_DIR=${PGSQUASH_OUTPUT_DIR:-$PGSQUASH_HOME/output}
    export PGSQUASH_LOGS_DIR=${PGSQUASH_LOGS_DIR:-$PGSQUASH_HOME/logs}
    export PGSQUASH_DOCKER_ENABLED=${PGSQUASH_DOCKER_ENABLED:-true}
    export PGSQUASH_AUTO_VALIDATE=${PGSQUASH_AUTO_VALIDATE:-true}
    export PGSQUASH_SAFETY_LEVEL=${PGSQUASH_SAFETY_LEVEL:-standard}

    # Create directories if they don't exist
    mkdir -p "$PGSQUASH_MIGRATIONS_DIR" "$PGSQUASH_OUTPUT_DIR" "$PGSQUASH_LOGS_DIR" "$(dirname "$PGSQUASH_CONFIG")"

    log_success "Environment setup complete"
}

# Docker setup
setup_docker() {
    log_step "Setting up Docker environment..."

    # Check if Docker is available
    if ! command -v docker &> /dev/null; then
        log_error "Docker is not available in this container"
        return 1
    fi

    # Check Docker socket permissions (Docker-in-Docker support)
    if [[ -S /var/run/docker.sock ]]; then
        local socket_gid=$(stat -c '%g' /var/run/docker.sock 2>/dev/null || echo "")
        if [[ -n "$socket_gid" ]] && [[ "$socket_gid" != "0" ]]; then
            log_info "Docker socket detected (GID: $socket_gid)"
            log_info "💡 Tip: Run with --group-add $socket_gid for Docker access"
        fi
    fi

    # Check Docker daemon connectivity (for Docker-in-Docker scenarios)
    if ! docker info &> /dev/null; then
        log_warning "Docker daemon not accessible - validation features will be limited"
        export PGSQUASH_DOCKER_ENABLED=false
        return 0
    fi

    log_success "Docker environment ready"

    # Pull required images if auto-setup is enabled
    if [[ "${PGSQUASH_AUTO_SETUP:-true}" == "true" ]]; then
        log_step "Pre-pulling required Docker images..."
        docker pull postgres:17 || log_warning "Failed to pull postgres:17"
        docker pull postgres:15 || log_warning "Failed to pull postgres:15"
        log_success "Docker images ready"
    fi
}

# Configuration setup
setup_config() {
    log_step "Setting up configuration..."

    if [[ ! -f "$PGSQUASH_CONFIG" ]]; then
        log_info "Creating default configuration..."

        # Generate config from template
        cat > "$PGSQUASH_CONFIG" << EOF
{
  "safety_level": "${PGSQUASH_SAFETY_LEVEL}",
  "output": {
    "directory": "${PGSQUASH_OUTPUT_DIR}",
    "format": "sql",
    "group_by": "semantic",
    "add_comments": true,
    "preserve_formatting": false
  },
  "rules": {
    "enable_external_dependency_filter": true,
    "enable_column_evolution": true,
    "enable_rls_consolidation": true,
    "enable_function_deduplication": true,
    "enable_dead_code_removal": false,
    "consolidation_rules": {
      "conservative": ["MultipleCreateConsolidationRule", "CreateAlterConsolidationRule"],
      "standard": ["MultipleCreateConsolidationRule", "CreateAlterConsolidationRule", "DropCreateCycleRule"],
      "aggressive": ["MultipleCreateConsolidationRule", "CreateAlterConsolidationRule", "DropCreateCycleRule", "FunctionDeduplicationRule"]
    }
  },
  "validation": {
    "enabled": ${PGSQUASH_AUTO_VALIDATE},
    "docker_enabled": ${PGSQUASH_DOCKER_ENABLED},
    "postgresql_versions": ["17", "16", "15", "14"],
    "timeout_minutes": 10,
    "parallel_validation": true
  },
  "performance": {
    "enable_streaming": true,
    "streaming_threshold": 100,
    "batch_size": 50,
    "memory_limit_mb": 512,
    "progress_reporting": true
  },
  "modern_features": {
    "enable_advanced_types": true,
    "enable_partitioning": true,
    "enable_json_operations": true,
    "postgresql_version": "17"
  },
  "ai": {
    "enabled": false,
    "provider": "claude",
    "max_retries": 3,
    "timeout_seconds": 30
  }
}
EOF
        log_success "Default configuration created"
    else
        log_info "Using existing configuration"
    fi

    # Validate configuration
    if pgsquash validate-config "$PGSQUASH_CONFIG" 2>/dev/null; then
        log_success "Configuration validated"
    else
        log_warning "Configuration validation failed - proceeding with defaults"
    fi
}

# Database setup for validation
setup_validation_db() {
    if [[ "${PGSQUASH_DOCKER_ENABLED}" != "true" ]]; then
        return 0
    fi

    log_step "Setting up validation database environment..."

    # Create Docker network for pgsquash if it doesn't exist
    if ! docker network inspect pgsquash-net &> /dev/null; then
        docker network create pgsquash-net
        log_success "Created Docker network: pgsquash-net"
    fi

    log_success "Validation environment ready"
}

# Initialize sample data if requested
initialize_sample_data() {
    if [[ "${PGSQUASH_INIT_SAMPLE:-false}" == "true" ]]; then
        log_step "Initializing sample migration data..."

        # Create sample migrations
        /app/scripts/create-sample-migrations.sh "$PGSQUASH_MIGRATIONS_DIR"
        log_success "Sample migration data created"
    fi
}

# Health check
perform_health_check() {
    log_step "Performing health check..."

    # Check binary
    if ! pgsquash --version &> /dev/null; then
        log_error "pgsquash binary not working"
        return 1
    fi

    # Check configuration
    if [[ ! -f "$PGSQUASH_CONFIG" ]]; then
        log_error "Configuration file not found"
        return 1
    fi

    # Check directories
    for dir in "$PGSQUASH_MIGRATIONS_DIR" "$PGSQUASH_OUTPUT_DIR" "$PGSQUASH_LOGS_DIR"; do
        if [[ ! -d "$dir" ]]; then
            log_error "Required directory not found: $dir"
            return 1
        fi
    done

    log_success "Health check passed"
}

# Show startup information
show_startup_info() {
    log_info "🔧 Configuration:"
    log_info "  ► Home Directory: $PGSQUASH_HOME"
    log_info "  ► Config File: $PGSQUASH_CONFIG"
    log_info "  ► Migrations: $PGSQUASH_MIGRATIONS_DIR"
    log_info "  ► Output: $PGSQUASH_OUTPUT_DIR"
    log_info "  ► Safety Level: $PGSQUASH_SAFETY_LEVEL"
    log_info "  ► Docker Enabled: $PGSQUASH_DOCKER_ENABLED"
    log_info "  ► Auto Validate: $PGSQUASH_AUTO_VALIDATE"
    echo
}

# Main initialization
init_pgsquash() {
    show_banner

    log_info "🚀 Starting pgsquash automated setup..."
    echo

    setup_environment
    setup_docker
    setup_config
    setup_validation_db
    initialize_sample_data
    perform_health_check

    echo
    log_success "☑ pgsquash setup completed successfully!"
    show_startup_info
}

# Command handlers
handle_init() {
    init_pgsquash
    log_success "🎉 pgsquash is ready to use!"
    echo
    log_info "💡 Quick start commands:"
    log_info "  ► Analyze migrations:    pgsquash analyze /app/migrations/*.sql"
    log_info "  ► Squash migrations:     pgsquash squash /app/migrations/*.sql --output /app/output/"
    log_info "  ► Validate with Docker:  pgsquash validate --docker /app/migrations/"
    log_info "  ► Full workflow:         pgsquash workflow --input /app/migrations/ --output /app/output/"
}

handle_squash() {
    init_pgsquash

    # Default squash command with all the bells and whistles
    local input_dir="${1:-$PGSQUASH_MIGRATIONS_DIR}"
    local output_dir="${2:-$PGSQUASH_OUTPUT_DIR}"

    log_info "🔄 Starting automated squashing workflow..."

    # Execute the squash command with comprehensive options
    pgsquash squash "$input_dir"/*.sql \
        --config "$PGSQUASH_CONFIG" \
        --output "$output_dir" \
        --safety-level "$PGSQUASH_SAFETY_LEVEL" \
        --validate \
        --progress \
        --verbose
}

handle_validate() {
    init_pgsquash

    local migrations_dir="${1:-$PGSQUASH_MIGRATIONS_DIR}"

    log_info "🧪 Starting Docker-based validation..."

    pgsquash validate \
        --docker \
        --input "$migrations_dir" \
        --config "$PGSQUASH_CONFIG" \
        --progress \
        --verbose
}

handle_workflow() {
    init_pgsquash

    local input_dir="${1:-$PGSQUASH_MIGRATIONS_DIR}"
    local output_dir="${2:-$PGSQUASH_OUTPUT_DIR}"

    log_info "🔄 Starting complete workflow (analyze → squash → validate)..."

    # Full workflow
    pgsquash workflow \
        --input "$input_dir" \
        --output "$output_dir" \
        --config "$PGSQUASH_CONFIG" \
        --safety-level "$PGSQUASH_SAFETY_LEVEL" \
        --validate \
        --docker \
        --progress \
        --verbose
}

# Trap handler for cleanup
cleanup() {
    log_info "🧹 Cleaning up..."

    # Clean up any running containers (only if Docker is enabled)
    if [[ "${PGSQUASH_DOCKER_ENABLED:-false}" == "true" ]]; then
        docker ps -q --filter "label=pgsquash" | xargs -r docker stop 2>/dev/null || true
        docker ps -a -q --filter "label=pgsquash" | xargs -r docker rm 2>/dev/null || true
    fi

    log_success "Cleanup completed"
}

trap cleanup EXIT

# Main command router
main() {
    local command="${1:-init}"

    case "$command" in
        "init")
            handle_init
            ;;
        "squash")
            shift
            handle_squash "$@"
            ;;
        "validate")
            shift
            handle_validate "$@"
            ;;
        "workflow")
            shift
            handle_workflow "$@"
            ;;
        "--help"|"-h"|"help")
            show_banner
            cat << 'EOF'
🚀 pgsquash Docker Container

USAGE:
    docker run --rm -v $(pwd)/migrations:/app/migrations -v $(pwd)/output:/app/output pgsquash [COMMAND] [OPTIONS]

COMMANDS:
    init        Initialize pgsquash environment (default)
    squash      Run squashing workflow on migrations
    validate    Validate migrations using Docker
    workflow    Run complete analyze → squash → validate workflow
    help        Show this help message

EXAMPLES:
    # Initialize and show help
    docker run --rm pgsquash init

    # Squash migrations from current directory
    docker run --rm -v $(pwd):/app/migrations pgsquash squash

    # Complete workflow with validation
    docker run --rm -v $(pwd):/app/migrations -v $(pwd)/output:/app/output pgsquash workflow

    # Docker validation (requires Docker socket)
    docker run --rm -v /var/run/docker.sock:/var/run/docker.sock -v $(pwd):/app/migrations pgsquash validate

ENVIRONMENT VARIABLES:
    PGSQUASH_SAFETY_LEVEL     Safety level: conservative|standard|aggressive (default: standard)
    PGSQUASH_AUTO_VALIDATE    Enable automatic validation (default: true)
    PGSQUASH_DOCKER_ENABLED   Enable Docker features (default: true)
    PGSQUASH_INIT_SAMPLE      Create sample migrations (default: false)

VOLUMES:
    /app/migrations     Input migration files
    /app/output         Output directory for squashed migrations
    /app/config         Configuration files
    /app/logs           Log files
    /var/run/docker.sock Docker socket (for validation features)

EOF
            ;;
        *)
            # Pass through to pgsquash binary
            exec pgsquash "$@"
            ;;
    esac
}

# Execute main function
main "$@"