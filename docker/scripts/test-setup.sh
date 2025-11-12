#!/bin/bash
set -euo pipefail

# Comprehensive Docker Setup Test Script
# Tests all aspects of the pgsquash Docker environment

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${BLUE}[TEST]${NC} $1" >&2; }
log_success() { echo -e "${GREEN}[PASS]${NC} $1" >&2; }
log_warning() { echo -e "${YELLOW}[WARN]${NC} $1" >&2; }
log_error() { echo -e "${RED}[FAIL]${NC} $1" >&2; }

# Test counters
TESTS_TOTAL=0
TESTS_PASSED=0
TESTS_FAILED=0

run_test() {
    local test_name="$1"
    local test_command="$2"

    ((TESTS_TOTAL++))
    log_info "Running: $test_name"

    if eval "$test_command"; then
        log_success "$test_name"
        ((TESTS_PASSED++))
        return 0
    else
        log_error "$test_name"
        ((TESTS_FAILED++))
        return 1
    fi
}

# Test Docker availability
test_docker_availability() {
    command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1
}

# Test Docker Compose availability
test_docker_compose_availability() {
    command -v docker-compose >/dev/null 2>&1 || docker compose version >/dev/null 2>&1
}

# Test Docker build
test_docker_build() {
    docker build -t pgsquash:test . >/dev/null 2>&1
}

# Test Docker run basic
test_docker_run_basic() {
    docker run --rm pgsquash:test --version >/dev/null 2>&1
}

# Test Docker entrypoint
test_docker_entrypoint() {
    docker run --rm pgsquash:test init >/dev/null 2>&1
}

# Test sample data generation
test_sample_data_generation() {
    local temp_dir=$(mktemp -d)
    docker run --rm -v "$temp_dir:/app/migrations" -e PGSQUASH_INIT_SAMPLE=true pgsquash:test init >/dev/null 2>&1
    local file_count=$(find "$temp_dir" -name "*.sql" | wc -l)
    rm -rf "$temp_dir"
    [[ $file_count -gt 5 ]]
}

# Test Docker Compose build
test_compose_build() {
    cp .env.example .env 2>/dev/null || true
    docker-compose build pgsquash >/dev/null 2>&1
}

# Test Docker Compose PostgreSQL
test_compose_postgres() {
    docker-compose up -d postgres-primary >/dev/null 2>&1
    sleep 10
    docker-compose exec -T postgres-primary pg_isready -U pgsquash >/dev/null 2>&1
}

# Test full workflow
test_full_workflow() {
    local temp_dir=$(mktemp -d)
    local output_dir=$(mktemp -d)

    # Generate sample data
    docker run --rm -v "$temp_dir:/app/migrations" -e PGSQUASH_INIT_SAMPLE=true pgsquash:test init >/dev/null 2>&1

    # Run squashing
    docker run --rm \
        -v "$temp_dir:/app/migrations" \
        -v "$output_dir:/app/output" \
        pgsquash:test squash >/dev/null 2>&1

    # Check output
    local output_files=$(find "$output_dir" -name "*.sql" | wc -l)

    # Cleanup
    rm -rf "$temp_dir" "$output_dir"

    [[ $output_files -gt 0 ]]
}

# Test Docker validation (requires Docker socket)
test_docker_validation() {
    if [[ ! -S /var/run/docker.sock ]]; then
        log_warning "Docker socket not available - skipping validation test"
        return 0
    fi

    local temp_dir=$(mktemp -d)

    # Generate minimal sample data
    mkdir -p "$temp_dir"
    cat > "$temp_dir/001_test.sql" << 'EOF'
CREATE TABLE test_table (id SERIAL PRIMARY KEY);
ALTER TABLE test_table ADD COLUMN name TEXT;
EOF

    # Run validation
    docker run --rm \
        -v "$temp_dir:/app/migrations" \
        -v /var/run/docker.sock:/var/run/docker.sock \
        --network host \
        pgsquash:test validate >/dev/null 2>&1

    local exit_code=$?
    rm -rf "$temp_dir"

    [[ $exit_code -eq 0 ]]
}

# Test multi-Platform build capability
test_multiPlatform_support() {
    # Check if buildx is available
    docker buildx inspect >/dev/null 2>&1 || return 1

    # Test that we can create a builder
    docker buildx create --name test-builder --use >/dev/null 2>&1 || return 1

    # Clean up
    docker buildx rm test-builder >/dev/null 2>&1 || true

    return 0
}

# Test environment configuration
test_environment_config() {
    # Test that .env.example exists and has required variables
    [[ -f .env.example ]] || return 1

    grep -q "PGSQUASH_SAFETY_LEVEL" .env.example || return 1
    grep -q "POSTGRES_PASSWORD" .env.example || return 1
    grep -q "DOCKER_COMPOSE_PROFILES" .env.example || return 1
}

# Test initialization script
test_init_script() {
    [[ -x scripts/init-pgsquash.sh ]] || return 1

    # Test help output
    scripts/init-pgsquash.sh --help >/dev/null 2>&1 || return 1
}

# Test Docker build script
test_build_script() {
    [[ -x docker/scripts/build.sh ]] || return 1

    # Test help output
    docker/scripts/build.sh --help >/dev/null 2>&1 || return 1
}

# Cleanup function
cleanup() {
    log_info "Cleaning up test environment..."

    # Stop any running containers
    docker-compose down >/dev/null 2>&1 || true

    # Remove test images
    docker rmi pgsquash:test >/dev/null 2>&1 || true

    # Remove test .env
    rm -f .env
}

# Main test execution
main() {
    cat << 'EOF'
🐳 pgsquash Docker Setup Test Suite

This script tests all aspects of the Docker-based pgsquash setup
to ensure everything works correctly like Supabase's initialization.

EOF

    log_info "Starting comprehensive Docker setup tests..."
    echo

    # Basic Docker tests
    run_test "Docker availability" "test_docker_availability"
    run_test "Docker Compose availability" "test_docker_compose_availability"
    run_test "Environment configuration" "test_environment_config"

    # Build tests
    run_test "Docker build" "test_docker_build"
    run_test "Multi-Platform support" "test_multiPlatform_support"

    # Runtime tests
    run_test "Docker run basic" "test_docker_run_basic"
    run_test "Docker entrypoint" "test_docker_entrypoint"
    run_test "Sample data generation" "test_sample_data_generation"

    # Compose tests
    run_test "Docker Compose build" "test_compose_build"
    run_test "Docker Compose PostgreSQL" "test_compose_postgres"

    # Workflow tests
    run_test "Full workflow" "test_full_workflow"
    run_test "Docker validation" "test_docker_validation"

    # Script tests
    run_test "Initialization script" "test_init_script"
    run_test "Build script" "test_build_script"

    # Summary
    echo
    log_info "Test Results Summary:"
    log_info "  ► Total Tests: $TESTS_TOTAL"
    log_success "  ► Passed: $TESTS_PASSED"

    if [[ $TESTS_FAILED -gt 0 ]]; then
        log_error "  ► Failed: $TESTS_FAILED"
        echo
        log_error "☒ Some tests failed. Please check the output above."
        exit 1
    else
        echo
        log_success "☑ All tests passed! pgsquash Docker setup is working correctly."

        cat << 'EOF'

🎉 Your pgsquash Docker environment is ready!

Next steps:
  1. Run the full setup: ./scripts/init-pgsquash.sh
  2. Or use Docker directly: docker run --rm pgsquash:test --help
  3. Or use Docker Compose: docker-compose up

For production deployment:
  1. Build and push: ./docker/scripts/build.sh --push
  2. Deploy with: docker-compose up -d

EOF
    fi
}

# Set up cleanup trap
trap cleanup EXIT

# Run main function
main "$@"