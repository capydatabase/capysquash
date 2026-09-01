#!/bin/bash
set -euo pipefail

# Multi-Version PostgreSQL Testing Script
# Tests migrations against multiple PostgreSQL versions
#
# NOTE: For manual testing, you can also use docker-compose.testing.yml which includes
# PostgreSQL 17, 16, and 15. This script provides automated testing across more versions
# with isolated containers and detailed reporting.

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
NC='\033[0m'

log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
log_warning() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }
log_step() { echo -e "${PURPLE}[STEP]${NC} $1"; }

# Configuration
MIGRATIONS_DIR="${1:-migrations}"
POSTGRES_VERSIONS=("17" "16" "15" "18")
OUTPUT_DIR="multi-version-test-results-$(date +%Y%m%d-%H%M%S)"

# Test results tracking
declare -A TEST_RESULTS

show_help() {
    cat << EOF
Usage: $0 [MIGRATIONS_DIR] [OPTIONS]

Test migrations against multiple PostgreSQL versions

ARGUMENTS:
    MIGRATIONS_DIR      Directory containing migrations (default: migrations)

OPTIONS:
    --versions VERS     Comma-separated PostgreSQL versions (default: 17,16,15,18)
    --quick             Test only latest and oldest versions
    -h, --help          Show this help message

EXAMPLES:
    # Test with default versions
    $0

    # Test specific directory
    $0 my-migrations/

    # Test specific versions
    $0 --versions 17,16,15

    # Quick test (latest and oldest only)
    $0 --quick

EOF
}

# Parse command line arguments
QUICK_MODE=false
shift 2>/dev/null || true  # Skip migrations dir if provided

while [[ $# -gt 0 ]]; do
    case $1 in
        --versions)
            IFS=',' read -ra POSTGRES_VERSIONS <<< "$2"
            shift 2
            ;;
        --quick)
            QUICK_MODE=true
            shift
            ;;
        -h|--help)
            show_help
            exit 0
            ;;
        *)
            shift
            ;;
    esac
done

# Quick mode: test only first and last version
if [ "$QUICK_MODE" = true ]; then
    POSTGRES_VERSIONS=("${POSTGRES_VERSIONS[0]}" "${POSTGRES_VERSIONS[-1]}")
    log_info "Quick mode: Testing PostgreSQL ${POSTGRES_VERSIONS[0]} and ${POSTGRES_VERSIONS[-1]}"
fi

# Check prerequisites
if [ ! -d "$MIGRATIONS_DIR" ]; then
    log_error "Migrations directory not found: $MIGRATIONS_DIR"
    exit 1
fi

if ! docker info > /dev/null 2>&1; then
    log_error "Docker is not running"
    exit 1
fi

# Create output directory
mkdir -p "$OUTPUT_DIR"

log_info "🧪 Multi-Version PostgreSQL Testing"
log_info "Migrations: $MIGRATIONS_DIR"
log_info "Testing versions: ${POSTGRES_VERSIONS[*]}"
log_info "Output: $OUTPUT_DIR"
echo ""

# Test function for a single PostgreSQL version
test_version() {
    local version=$1
    local container_name="pgsquash-test-pg${version}"

    log_step "Testing PostgreSQL $version..."

    # Start PostgreSQL container
    log_info "Starting PostgreSQL $version container..."
    docker run -d \
        --name "$container_name" \
        --label "pgsquash.test=multi-version" \
        -e POSTGRES_PASSWORD=test \
        -e POSTGRES_USER=postgres \
        -e POSTGRES_DB=test \
        postgres:${version} > /dev/null 2>&1

    # Wait for readiness
    local attempts=0
    while [ $attempts -lt 30 ]; do
        if docker exec "$container_name" pg_isready -U postgres > /dev/null 2>&1; then
            break
        fi
        sleep 1
        ((attempts++))
    done

    if [ $attempts -eq 30 ]; then
        log_error "PostgreSQL $version failed to start"
        TEST_RESULTS[$version]="FAILED: Container startup timeout"
        docker rm -f "$container_name" > /dev/null 2>&1 || true
        return 1
    fi

    # Apply migrations
    local error_count=0
    local total_files=0
    local version_log="$OUTPUT_DIR/pg${version}.log"

    echo "PostgreSQL $version Test Results" > "$version_log"
    echo "=================================" >> "$version_log"
    echo "" >> "$version_log"

    for migration_file in "$MIGRATIONS_DIR"/*.sql; do
        if [ -f "$migration_file" ]; then
            ((total_files++))
            local migration_name=$(basename "$migration_file")

            log_info "  Applying $migration_name..."

            if docker exec -i "$container_name" psql -U postgres -d test < "$migration_file" >> "$version_log" 2>&1; then
                echo "☑ $migration_name" >> "$version_log"
            else
                echo "✗ $migration_name" >> "$version_log"
                ((error_count++))
                log_warning "  Failed: $migration_name"
            fi
        fi
    done

    # Schema analysis
    echo "" >> "$version_log"
    echo "Schema Summary:" >> "$version_log"
    echo "===============" >> "$version_log"

    docker exec "$container_name" psql -U postgres -d test -c "
        SELECT 'Tables: ' || COUNT(*)::text
        FROM information_schema.tables
        WHERE table_schema = 'public'
    " >> "$version_log" 2>&1

    docker exec "$container_name" psql -U postgres -d test -c "
        SELECT 'Functions: ' || COUNT(*)::text
        FROM information_schema.routines
        WHERE routine_schema = 'public'
    " >> "$version_log" 2>&1

    docker exec "$container_name" psql -U postgres -d test -c "
        SELECT 'Extensions: ' || COUNT(*)::text
        FROM pg_extension
        WHERE extname != 'plpgsql'
    " >> "$version_log" 2>&1

    # Cleanup
    docker rm -f "$container_name" > /dev/null 2>&1

    # Record result
    if [ $error_count -eq 0 ]; then
        log_success "PostgreSQL $version: PASSED ($total_files migrations)"
        TEST_RESULTS[$version]="PASSED: $total_files migrations applied"
    else
        log_error "PostgreSQL $version: FAILED ($error_count/$total_files errors)"
        TEST_RESULTS[$version]="FAILED: $error_count errors in $total_files migrations"
    fi

    return $error_count
}

# Run tests for each version
FAILED_VERSIONS=()
for version in "${POSTGRES_VERSIONS[@]}"; do
    if ! test_version "$version"; then
        FAILED_VERSIONS+=("$version")
    fi
    echo ""
done

# Generate summary report
SUMMARY_FILE="$OUTPUT_DIR/summary.txt"

{
    echo "=========================================="
    echo "Multi-Version PostgreSQL Test Summary"
    echo "=========================================="
    echo ""
    echo "Test Date: $(date)"
    echo "Migrations Directory: $MIGRATIONS_DIR"
    echo "Versions Tested: ${POSTGRES_VERSIONS[*]}"
    echo ""
    echo "Results:"
    echo "--------"

    for version in "${POSTGRES_VERSIONS[@]}"; do
        echo "PostgreSQL $version: ${TEST_RESULTS[$version]}"
    done

    echo ""
    if [ ${#FAILED_VERSIONS[@]} -eq 0 ]; then
        echo "☑ All versions PASSED"
    else
        echo "☒ Failed versions: ${FAILED_VERSIONS[*]}"
    fi

    echo ""
    echo "Detailed logs available in: $OUTPUT_DIR/"

} > "$SUMMARY_FILE"

# Display summary
cat "$SUMMARY_FILE"
echo ""

# Cleanup any remaining test containers
log_info "Cleaning up test containers..."
docker rm -f $(docker ps -aq -f "label=pgsquash.test=multi-version" 2>/dev/null) > /dev/null 2>&1 || true

# Exit with appropriate code
if [ ${#FAILED_VERSIONS[@]} -eq 0 ]; then
    log_success "☑ All PostgreSQL versions passed!"
    exit 0
else
    log_error "☒ Some versions failed. Check logs in: $OUTPUT_DIR/"
    exit 1
fi
