#!/bin/bash
# Docker Validation Runner for E2E Test Results
# Validates all squashed scenarios against PostgreSQL
# Generated: 2025-10-28

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
MIGRATIONS_DIR="$PROJECT_ROOT/migrations"
SQUASHED_BASE_DIR="$PROJECT_ROOT/squashed"
VALIDATION_LOG_DIR="$SCRIPT_DIR/validation-logs"

mkdir -p "$VALIDATION_LOG_DIR"

# Test scenarios to validate
SCENARIOS=(
  "standard-no-ai"
  "standard-ai"
  "conservative-no-ai"
  "conservative-ai"
  "aggressive-no-ai"
  "aggressive-ai"
)

# Results file
VALIDATION_RESULTS="$SCRIPT_DIR/docker-validation-results.txt"

# Initialize results
echo "===========================================" >  "$VALIDATION_RESULTS"
echo "DOCKER VALIDATION RESULTS" >> "$VALIDATION_RESULTS"
echo "Started: $(date)" >> "$VALIDATION_RESULTS"
echo "===========================================" >> "$VALIDATION_RESULTS"
echo "" >> "$VALIDATION_RESULTS"

# Function to validate a scenario
validate_scenario() {
  local scenario=$1
  local output_dir="$SQUASHED_BASE_DIR/${scenario}"
  local log_file="$VALIDATION_LOG_DIR/${scenario}.log"

  echo -e "${BLUE}[$(date +%H:%M:%S)]${NC} Validating ${YELLOW}${scenario}${NC}..."

  # Check if squashed output exists
  if [ ! -d "$output_dir" ]; then
    echo -e "${RED}✗${NC} Output directory not found: $output_dir"
    echo "Scenario: $scenario - SKIPPED (no output)" >> "$VALIDATION_RESULTS"
    return 1
  fi

  # Check for baseline file
  if [ ! -f "$output_dir/000_baseline.sql" ]; then
    echo -e "${RED}✗${NC} Baseline file not found"
    echo "Scenario: $scenario - FAILED (no baseline)" >> "$VALIDATION_RESULTS"
    return 1
  fi

  # Run validation
  local start_time=$(date +%s)

  if "$PROJECT_ROOT/scripts/validate.sh" \
    --mode full \
    --migrations "$MIGRATIONS_DIR" \
    --output "$output_dir" \
    > "$log_file" 2>&1; then

    local end_time=$(date +%s)
    local duration=$((end_time - start_time))

    # Extract metrics from log
    local original_tables=$(grep "Tables - Original:" "$log_file" | grep -oP 'Original: \K[0-9]+' || echo "N/A")
    local squashed_tables=$(grep "Tables - Squashed:" "$log_file" | grep -oP 'Squashed: \K[0-9]+' || echo "N/A")
    local original_functions=$(grep "Functions - Original:" "$log_file" | grep -oP 'Original: \K[0-9]+' || echo "N/A")
    local squashed_functions=$(grep "Functions - Squashed:" "$log_file" | grep -oP 'Squashed: \K[0-9]+' || echo "N/A")

    echo -e "${GREEN}✓${NC} Validation passed in ${duration}s"
    echo -e "  Tables: ${original_tables} → ${squashed_tables}"
    echo -e "  Functions: ${original_functions} → ${squashed_functions}"

    {
      echo "Scenario: $scenario"
      echo "  Status: PASSED"
      echo "  Duration: ${duration}s"
      echo "  Original Tables: $original_tables"
      echo "  Squashed Tables: $squashed_tables"
      echo "  Original Functions: $original_functions"
      echo "  Squashed Functions: $squashed_functions"
      echo ""
    } >> "$VALIDATION_RESULTS"

    return 0
  else
    local end_time=$(date +%s)
    local duration=$((end_time - start_time))

    echo -e "${RED}✗${NC} Validation failed after ${duration}s"
    echo -e "  Check log: $log_file"

    {
      echo "Scenario: $scenario"
      echo "  Status: FAILED"
      echo "  Duration: ${duration}s"
      echo "  Log: $log_file"
      echo ""
    } >> "$VALIDATION_RESULTS"

    return 1
  fi
}

# Main execution
main() {
  echo -e "${BLUE}=========================================${NC}"
  echo -e "${BLUE}Docker Validation Runner${NC}"
  echo -e "${BLUE}=========================================${NC}"
  echo ""

  local success_count=0
  local fail_count=0
  local skip_count=0
  local total_count=${#SCENARIOS[@]}

  for scenario in "${SCENARIOS[@]}"; do
    if validate_scenario "$scenario"; then
      ((success_count++))
    else
      if [ -d "$SQUASHED_BASE_DIR/${scenario}" ]; then
        ((fail_count++))
      else
        ((skip_count++))
      fi
    fi
    echo ""
  done

  # Summary
  echo -e "${BLUE}=========================================${NC}"
  echo -e "${BLUE}Validation Summary${NC}"
  echo -e "${BLUE}=========================================${NC}"
  echo -e "Total scenarios: ${total_count}"
  echo -e "Passed: ${GREEN}${success_count}${NC}"
  echo -e "Failed: ${RED}${fail_count}${NC}"
  echo -e "Skipped: ${YELLOW}${skip_count}${NC}"
  echo ""
  echo -e "Results saved to: ${YELLOW}${VALIDATION_RESULTS}${NC}"
  echo -e "Logs available in: ${YELLOW}${VALIDATION_LOG_DIR}/${NC}"
  echo ""

  # Write completion
  {
    echo "==========================================="
    echo "Completed: $(date)"
    echo "Summary: $success_count passed, $fail_count failed, $skip_count skipped"
    echo "==========================================="
  } >> "$VALIDATION_RESULTS"

  # Exit with error if any failures
  if [ "$fail_count" -gt 0 ]; then
    exit 1
  fi
}

main "$@"
