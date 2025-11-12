#!/bin/bash
# pgsquash Common Library
# Shared functions and utilities for all pgsquash scripts

# ============================================================================
# COLOR DEFINITIONS
# ============================================================================
export RED='\033[0;31m'
export GREEN='\033[0;32m'
export YELLOW='\033[1;33m'
export BLUE='\033[0;34m'
export PURPLE='\033[0;35m'
export CYAN='\033[0;36m'
export NC='\033[0m' # No Color

# ============================================================================
# LOGGING FUNCTIONS
# ============================================================================

log_info() {
  echo -e "${CYAN}[INFO]${NC} $1" >&2
}

log_success() {
  echo -e "${GREEN}[SUCCESS]${NC} $1" >&2
}

log_warning() {
  echo -e "${YELLOW}[WARNING]${NC} $1" >&2
}

log_error() {
  echo -e "${RED}[ERROR]${NC} $1" >&2
}

log_step() {
  echo -e "${PURPLE}[STEP]${NC} $1" >&2
}

log_substep() {
  echo -e "${BLUE}  →${NC} $1" >&2
}

log_fatal() {
  echo -e "${RED}[FATAL]${NC} $1" >&2
  exit 1
}

# ============================================================================
# DOCKER UTILITIES
# ============================================================================

check_docker() {
  if ! command -v docker &> /dev/null; then
    log_fatal "Docker is not installed or not in PATH"
  fi

  if ! docker info > /dev/null 2>&1; then
    log_fatal "Docker is not running or not accessible"
  fi

  log_success "Docker is available"
}

check_docker_compose() {
  if ! command -v docker-compose &> /dev/null && ! docker compose version &> /dev/null 2>&1; then
    log_fatal "Docker Compose is not installed"
  fi
  log_success "Docker Compose is available"
}

wait_for_container() {
  local container_name="$1"
  local timeout="${2:-60}"
  local attempts=0

  log_info "Waiting for container $container_name to be ready..."

  while [ $attempts -lt "$timeout" ]; do
    if docker exec "$container_name" pg_isready -U postgres > /dev/null 2>&1; then
      log_success "Container $container_name is ready"
      return 0
    fi
    sleep 1
    ((attempts++))
  done

  log_error "Container $container_name failed to start within $timeout seconds"
  return 1
}

cleanup_container() {
  local container_name="$1"

  if docker ps -q -f name="$container_name" | grep -q .; then
    log_info "Cleaning up container: $container_name"
    docker stop "$container_name" > /dev/null 2>&1
    docker rm "$container_name" > /dev/null 2>&1
    log_success "Container removed: $container_name"
  fi
}

# ============================================================================
# FILE & DIRECTORY UTILITIES
# ============================================================================

ensure_dir() {
  local dir="$1"
  if [ ! -d "$dir" ]; then
    mkdir -p "$dir"
    log_info "Created directory: $dir"
  fi
}

check_file_exists() {
  local file="$1"
  if [ ! -f "$file" ]; then
    log_fatal "Required file not found: $file"
  fi
}

check_dir_exists() {
  local dir="$1"
  if [ ! -d "$dir" ]; then
    log_fatal "Required directory not found: $dir"
  fi
}

# ============================================================================
# POSTGRES UTILITIES
# ============================================================================

exec_postgres_sql() {
  local container_name="$1"
  local database="$2"
  local sql="$3"

  docker exec "$container_name" psql -U postgres -d "$database" -c "$sql" 2>&1
}

apply_migration_file() {
  local container_name="$1"
  local database="$2"
  local migration_file="$3"

  if [ ! -f "$migration_file" ]; then
    log_error "Migration file not found: $migration_file"
    return 1
  fi

  if docker exec -i "$container_name" psql -U postgres -d "$database" < "$migration_file" > /dev/null 2>&1; then
    return 0
  else
    return 1
  fi
}

get_table_count() {
  local container_name="$1"
  local database="$2"

  docker exec "$container_name" psql -U postgres -d "$database" -t -c \
    "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = 'public';" \
    2>/dev/null | xargs
}

get_function_count() {
  local container_name="$1"
  local database="$2"

  docker exec "$container_name" psql -U postgres -d "$database" -t -c \
    "SELECT COUNT(*) FROM information_schema.routines WHERE routine_schema = 'public';" \
    2>/dev/null | xargs
}

# ============================================================================
# ENVIRONMENT & CONFIGURATION
# ============================================================================

load_env_file() {
  local env_file="$1"

  if [ -f "$env_file" ]; then
    # shellcheck disable=SC2046
    export $(grep -v '^#' "$env_file" | xargs)
    log_success "Environment loaded from $env_file"
  else
    log_warning "Environment file not found: $env_file"
  fi
}

detect_project_root() {
  local current_dir
  current_dir="$(pwd)"

  while [ "$current_dir" != "/" ]; do
    if [ -f "$current_dir/go.mod" ]; then
      echo "$current_dir"
      return 0
    fi
    current_dir="$(dirname "$current_dir")"
  done

  log_error "Could not detect project root (no go.mod found)"
  return 1
}

# ============================================================================
# VALIDATION UTILITIES
# ============================================================================

compare_schema_counts() {
  local container_name="$1"
  local original_db="$2"
  local squashed_db="$3"

  local original_tables original_functions
  local squashed_tables squashed_functions

  original_tables=$(get_table_count "$container_name" "$original_db")
  squashed_tables=$(get_table_count "$container_name" "$squashed_db")

  original_functions=$(get_function_count "$container_name" "$original_db")
  squashed_functions=$(get_function_count "$container_name" "$squashed_db")

  log_info "Schema comparison results:"
  echo "  Tables - Original: $original_tables, Squashed: $squashed_tables"
  echo "  Functions - Original: $original_functions, Squashed: $squashed_functions"

  local table_diff=$((original_tables - squashed_tables))
  local function_diff=$((original_functions - squashed_functions))

  # Allow some differences for consolidation
  if [ "$table_diff" -gt 5 ] || [ "$function_diff" -gt 10 ]; then
    log_warning "Significant differences detected in object counts"
    return 1
  else
    log_success "Schema object counts are within expected ranges"
    return 0
  fi
}

# ============================================================================
# BANNER & HELP UTILITIES
# ============================================================================

show_pgsquash_banner() {
  cat << 'EOF'
                                                                      $$\
                                                                      $$ |
 $$$$$$\   $$$$$$\   $$$$$$$\  $$$$$$\  $$\   $$\  $$$$$$\   $$$$$$$\ $$$$$$$\
$$  __$$\ $$  __$$\ $$  _____|$$  __$$\ $$ |  $$ | \____$$\ $$  _____|$$  __$$\
$$ /  $$ |$$ /  $$ |\$$$$$$\  $$ /  $$ |$$ |  $$ | $$$$$$$ |\$$$$$$\  $$ |  $$ |
$$ |  $$ |$$ |  $$ | \____$$\ $$ |  $$ |$$ |  $$ |$$  __$$ | \____$$\ $$ |  $$ |
$$$$$$$  |\$$$$$$$ |$$$$$$$  |\$$$$$$$ |\$$$$$$  |\$$$$$$$ |$$$$$$$  |$$ |  $$ |
$$  ____/  \____$$ |\______/  \____$$ | \______/  \_______|\______/ \__|  \__|
$$ |      $$\   $$ |                $$ |
$$ |      \$$$$$$  |                $$ |
\__|       \______/                 \__|

   🚀 PostgreSQL Migration Squasher
EOF
}

# ============================================================================
# CONFIRMATION UTILITIES
# ============================================================================

confirm_action() {
  local prompt="$1"
  local default="${2:-n}"

  local yn_prompt="[y/N]"
  [ "$default" = "y" ] && yn_prompt="[Y/n]"

  read -p "$prompt $yn_prompt " -n 1 -r
  echo

  if [[ $REPLY =~ ^[Yy]$ ]]; then
    return 0
  else
    return 1
  fi
}

# ============================================================================
# ERROR HANDLING
# ============================================================================

trap_handler() {
  local exit_code=$?
  if [ $exit_code -ne 0 ]; then
    log_error "Script failed with exit code: $exit_code"
  fi
}

setup_error_handling() {
  set -euo pipefail
  trap trap_handler EXIT
}

# ============================================================================
# VERSION CHECKING
# ============================================================================

get_script_version() {
  echo "0.9.7"
}

check_prerequisites() {
  local required_commands=("$@")

  for cmd in "${required_commands[@]}"; do
    if ! command -v "$cmd" &> /dev/null; then
      log_error "Required command not found: $cmd"
      return 1
    fi
  done

  log_success "All prerequisites met"
  return 0
}

# ============================================================================
# INITIALIZATION
# ============================================================================

# Export all functions
export -f log_info log_success log_warning log_error log_step log_substep log_fatal
export -f check_docker check_docker_compose wait_for_container cleanup_container
export -f ensure_dir check_file_exists check_dir_exists
export -f exec_postgres_sql apply_migration_file get_table_count get_function_count
export -f load_env_file detect_project_root compare_schema_counts
export -f show_pgsquash_banner confirm_action trap_handler setup_error_handling
export -f get_script_version check_prerequisites

log_info "pgsquash common library loaded (v$(get_script_version))"
