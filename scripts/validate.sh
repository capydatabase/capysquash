#!/bin/bash
# Unified Validation Script for pgsquash
# Consolidates: validate.sh, quick-validate.sh, schema-diff.sh, setup-validation.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
source "$SCRIPT_DIR/lib/common.sh"

setup_error_handling

# ============================================================================
# CONFIGURATION
# ============================================================================

VALIDATION_MODE="${VALIDATION_MODE:-full}"
MIGRATIONS_DIR="${1:-migrations}"
OUTPUT_DIR="${OUTPUT_DIR:-./output}"
CONTAINER_PREFIX="pgsquash-validation"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-pgsquash_secure_password}"
ORIGINAL_DB="original_migrations"
SQUASHED_DB="squashed_migrations"
KEEP_CONTAINER=false
TIMEOUT=60

# ============================================================================
# HELP & USAGE
# ============================================================================

show_help() {
  cat << EOF
USAGE:
  $0 [MIGRATIONS_DIR] [OPTIONS]

DESCRIPTION:
  Unified validation script supporting multiple validation modes

MODES:
  --mode quick           Quick validation using docker-compose
  --mode full            Full validation with schema comparison (default)
  --mode schema-diff     Detailed schema difference analysis
  --mode setup           Auto-detect extensions and create validation environment

OPTIONS:
  --migrations DIR       Migrations directory (default: migrations)
  --output DIR           Output directory (default: ./output)
  --keep-container       Keep container running for manual inspection
  --container-prefix     Container name prefix (default: pgsquash-validation)
  --timeout SECONDS      Startup timeout (default: 60)
  -h, --help             Show this help message

EXAMPLES:
  # Quick validation
  $0 --mode quick

  # Full validation with custom migrations
  $0 my-migrations/ --mode full

  # Schema difference analysis
  $0 --mode schema-diff --keep-container

  # Auto-setup validation environment
  $0 --mode setup --output /tmp/validation

EOF
}

# ============================================================================
# ARGUMENT PARSING
# ============================================================================

parse_arguments() {
  while [[ $# -gt 0 ]]; do
    case $1 in
      --mode)
        VALIDATION_MODE="$2"
        shift 2
        ;;
      --migrations)
        MIGRATIONS_DIR="$2"
        shift 2
        ;;
      --output)
        OUTPUT_DIR="$2"
        shift 2
        ;;
      --keep-container)
        KEEP_CONTAINER=true
        shift
        ;;
      --container-prefix)
        CONTAINER_PREFIX="$2"
        shift 2
        ;;
      --timeout)
        TIMEOUT="$2"
        shift 2
        ;;
      -h|--help)
        show_help
        exit 0
        ;;
      *)
        if [ -d "$1" ]; then
          MIGRATIONS_DIR="$1"
        fi
        shift
        ;;
    esac
  done
}

# ============================================================================
# MODE: QUICK VALIDATION
# ============================================================================

mode_quick_validate() {
  log_step "Running quick validation..."

  check_dir_exists "$MIGRATIONS_DIR"
  check_docker

  local compose_file="docker/deployment/with-validation.yml"

  if [ -f "$compose_file" ]; then
    log_info "Using compose file: $compose_file"

    export MIGRATIONS_DIR
    export OUTPUT_DIR
    export PGSQUASH_SAFETY_LEVEL="${PGSQUASH_SAFETY_LEVEL:-standard}"
    export PGSQUASH_AUTO_VALIDATE="true"
    export PGSQUASH_DOCKER_ENABLED="true"

    log_info "Running validation with Docker Compose..."
    docker compose -f "$compose_file" run --rm pgsquash validate "$MIGRATIONS_DIR/*.sql"

    log_success "☑ Quick validation completed successfully!"
  else
    log_warning "Compose file not found, using direct Docker..."

    if ! docker images | grep -q "pgsquash"; then
      log_info "Building pgsquash image..."
      docker build -t pgsquash:latest .
    fi

    docker run --rm \
      -v "$(pwd)/$MIGRATIONS_DIR:/app/migrations:ro" \
      -v "$(pwd)/output:/app/output" \
      -v /var/run/docker.sock:/var/run/docker.sock \
      --network host \
      -e PGSQUASH_DOCKER_ENABLED=true \
      -e PGSQUASH_AUTO_VALIDATE=true \
      pgsquash:latest validate /app/migrations/*.sql

    log_success "☑ Quick validation completed successfully!"
  fi

  if [ -d "output" ]; then
    log_info "📊 Results available in: output/"
    ls -lh output/
  fi
}

# ============================================================================
# MODE: FULL VALIDATION
# ============================================================================

start_postgres_container() {
  local container_name="$CONTAINER_PREFIX-postgres"

  if docker ps -q -f name="$container_name" | grep -q .; then
    log_info "PostgreSQL container already running"
    return 0
  fi

  log_info "Starting PostgreSQL container..."

  docker run -d \
    --name "$container_name" \
    -e POSTGRES_PASSWORD="$POSTGRES_PASSWORD" \
    -e POSTGRES_USER=postgres \
    -e POSTGRES_DB=postgres \
    -p 5433:5432 \
    postgres:17

  wait_for_container "$container_name" "$TIMEOUT"

  log_info "Creating validation databases..."
  docker exec "$container_name" psql -U postgres -c "SELECT 1 FROM pg_database WHERE datname = '$ORIGINAL_DB'" | grep -q 1 || \
    docker exec "$container_name" psql -U postgres -c "CREATE DATABASE $ORIGINAL_DB;"
  docker exec "$container_name" psql -U postgres -c "SELECT 1 FROM pg_database WHERE datname = '$SQUASHED_DB'" | grep -q 1 || \
    docker exec "$container_name" psql -U postgres -c "CREATE DATABASE $SQUASHED_DB;"

  log_success "PostgreSQL validation environment ready"
}

apply_original_migrations() {
  local container_name="$CONTAINER_PREFIX-postgres"
  local error_count=0
  local total_files=0

  log_info "Applying original migrations..."

  for migration_file in "$MIGRATIONS_DIR"/*.sql; do
    if [ -f "$migration_file" ]; then
      ((total_files++))
      local filename
      filename=$(basename "$migration_file")
      log_info "  Applying $filename..."

      if apply_migration_file "$container_name" "$ORIGINAL_DB" "$migration_file"; then
        :
      else
        ((error_count++))
        log_warning "  Failed: $filename"
      fi
    fi
  done

  log_info "Original migrations applied: $total_files files, $error_count with errors"
  return "$error_count"
}

apply_squashed_migration() {
  local container_name="$CONTAINER_PREFIX-postgres"

  log_info "Applying squashed migrations..."

  # Check for both new format (000_baseline.sql + 010_data.sql) and legacy format (001_consolidated_migration.sql)
  local baseline_file="$OUTPUT_DIR/000_baseline.sql"
  local data_file="$OUTPUT_DIR/010_data.sql"
  local legacy_file="$OUTPUT_DIR/001_consolidated_migration.sql"

  if [ -f "$baseline_file" ]; then
    # New format: apply baseline first
    log_info "Applying baseline migration..."
    if ! apply_migration_file "$container_name" "$SQUASHED_DB" "$baseline_file"; then
      log_error "Baseline migration failed"
      return 1
    fi

    # Apply data operations if present
    if [ -f "$data_file" ]; then
      log_info "Applying data operations..."
      if ! apply_migration_file "$container_name" "$SQUASHED_DB" "$data_file"; then
        log_error "Data operations migration failed"
        return 1
      fi
    fi

    log_success "Squashed migrations applied successfully"
    return 0

  elif [ -f "$legacy_file" ]; then
    # Legacy format: single consolidated file
    log_info "Applying legacy consolidated migration..."
    if apply_migration_file "$container_name" "$SQUASHED_DB" "$legacy_file"; then
      log_success "Squashed migration applied successfully"
      return 0
    else
      log_error "Squashed migration failed"
      return 1
    fi

  else
    log_error "No squashed migration files found in $OUTPUT_DIR"
    log_error "Expected: $baseline_file or $legacy_file"
    return 1
  fi
}

mode_full_validation() {
  log_step "Running full validation..."

  check_dir_exists "$MIGRATIONS_DIR"
  check_docker

  if [ "$KEEP_CONTAINER" = false ]; then
    trap 'cleanup_container "$CONTAINER_PREFIX-postgres"' EXIT
  else
    trap 'log_info "Container kept for inspection: $CONTAINER_PREFIX-postgres"' EXIT
  fi

  start_postgres_container

  local original_errors=0
  local squashed_errors=0
  local comparison_result=0

  apply_original_migrations || original_errors=$?
  apply_squashed_migration || squashed_errors=$?

  compare_schema_counts "$CONTAINER_PREFIX-postgres" "$ORIGINAL_DB" "$SQUASHED_DB" || comparison_result=$?

  echo ""
  log_info "=== Validation Summary ==="

  if [ $original_errors -eq 0 ]; then
    log_success "Original migrations: Applied successfully"
  else
    log_warning "Original migrations: $original_errors files had errors"
  fi

  if [ $squashed_errors -eq 0 ]; then
    log_success "Squashed migration: Applied successfully"
  else
    log_error "Squashed migration: Failed to apply"
  fi

  if [ $comparison_result -eq 0 ]; then
    log_success "Schema comparison: Passed"
  else
    log_warning "Schema comparison: Differences detected"
  fi

  if [ $squashed_errors -eq 0 ] && [ $comparison_result -eq 0 ]; then
    log_success "☑ Validation PASSED: Squashed migration produces equivalent schema"
    exit 0
  else
    log_error "☒ Validation FAILED: Issues detected in squashed migration"
    exit 1
  fi
}

# ============================================================================
# MODE: SCHEMA DIFF
# ============================================================================

mode_schema_diff() {
  log_step "Running schema diff analysis..."

  check_docker

  local container_name="$CONTAINER_PREFIX-postgres"

  if ! docker ps -q -f name="$container_name" | grep -q .; then
    log_error "Validation container not found. Run --mode full first."
    exit 1
  fi

  log_info "Analyzing schemas..."

  for mode in conservative paranoid standard aggressive; do
    local test_db="test_$mode"
    local mode_upper
    mode_upper=$(echo "$mode" | tr '[:lower:]' '[:upper:]')

    log_step "Analyzing $mode_upper mode..."

    log_info "Object counts for $mode_upper:"
    docker exec "$container_name" psql -U postgres -d "$test_db" -t -c "
      SELECT 'Tables: ' || COUNT(*) FROM information_schema.tables WHERE table_schema = 'public';
      SELECT 'Functions: ' || COUNT(*) FROM information_schema.routines WHERE routine_schema = 'public';
      SELECT 'Indexes: ' || COUNT(*) FROM pg_indexes WHERE schemaname = 'public';
    " 2>/dev/null || log_warning "Could not query $test_db"

    echo ""
  done

  log_success "Schema diff analysis completed"
}

# ============================================================================
# MODE: SETUP VALIDATION ENVIRONMENT
# ============================================================================

mode_setup_validation() {
  log_step "Setting up validation environment..."

  check_dir_exists "$MIGRATIONS_DIR"
  ensure_dir "$OUTPUT_DIR"

  log_info "Analyzing migrations for extension requirements..."

  # Build pgsquash if needed
  if [ ! -f "pgsquash" ]; then
    log_info "Building pgsquash..."
    go build -o pgsquash cmd/pgsquash/main.go
  fi

  # Generate squashed migration
  ./pgsquash squash "${MIGRATIONS_DIR}"/*.sql --output "$OUTPUT_DIR/squashed.sql" > "$OUTPUT_DIR/analysis.log" 2>&1

  log_info "Creating Docker Compose configuration..."

  cat > "$OUTPUT_DIR/docker-compose.yml" << 'EOF'
version: '3.8'

services:
  postgres-original:
    image: postgres:17
    container_name: pgsquash-original
    environment:
      POSTGRES_DB: postgres
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
    ports:
      - "5432:5432"
    volumes:
      - ./migrations:/migrations
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]
      interval: 10s
      timeout: 5s
      retries: 5

  postgres-squashed:
    image: postgres:17
    container_name: pgsquash-squashed
    environment:
      POSTGRES_DB: postgres
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres
    ports:
      - "5433:5432"
    volumes:
      - ./squashed.sql:/migrations
    depends_on:
      postgres-original:
        condition: service_healthy
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres"]
      interval: 10s
      timeout: 5s
      retries: 5
EOF

  log_success "☑ Validation environment setup complete!"
  log_info "Output directory: $OUTPUT_DIR"
  log_info ""
  log_info "To run validation:"
  log_info "  cd $OUTPUT_DIR"
  log_info "  docker-compose up"
}

# ============================================================================
# MAIN EXECUTION
# ============================================================================

main() {
  show_pgsquash_banner
  echo ""

  parse_arguments "$@"

  log_info "🔍 Validation Mode: $VALIDATION_MODE"
  log_info "📁 Migrations: $MIGRATIONS_DIR"
  log_info "📊 Output: $OUTPUT_DIR"
  echo ""

  case "$VALIDATION_MODE" in
    quick)
      mode_quick_validate
      ;;
    full)
      mode_full_validation
      ;;
    schema-diff)
      mode_schema_diff
      ;;
    setup)
      mode_setup_validation
      ;;
    *)
      log_error "Unknown validation mode: $VALIDATION_MODE"
      show_help
      exit 1
      ;;
  esac
}

main "$@"
