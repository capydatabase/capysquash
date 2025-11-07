#!/bin/bash
# E2E Validation Test Runner
# Executes all 8 test scenarios (paranoid/conservative/standard/aggressive × AI-enabled/AI-disabled)
# Generated: 2025-10-28

set -e  # Exit on error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
MIGRATIONS_DIR="$PROJECT_ROOT/migrations"
TEST_CONFIGS_DIR="$PROJECT_ROOT/test-configs"
SQUASHED_BASE_DIR="$PROJECT_ROOT/squashed"
RESULTS_DIR="$SCRIPT_DIR"
LOG_DIR="$RESULTS_DIR/logs"

# Create necessary directories
mkdir -p "$LOG_DIR"
mkdir -p "$SQUASHED_BASE_DIR"

# Test scenarios
SCENARIOS=(
  "paranoid-no-ai"
  "paranoid-ai"
  "conservative-no-ai"
  "conservative-ai"
  "standard-no-ai"
  "standard-ai"
  "aggressive-no-ai"
  "aggressive-ai"
)

# Results tracking
RESULTS_FILE="$RESULTS_DIR/execution-results.txt"
METRICS_FILE="$RESULTS_DIR/consolidation-metrics.csv"

# Initialize results files
echo "===========================================">  "$RESULTS_FILE"
echo "E2E VALIDATION EXECUTION RESULTS" >> "$RESULTS_FILE"
echo "Started: $(date)" >> "$RESULTS_FILE"
echo "===========================================" >> "$RESULTS_FILE"
echo "" >> "$RESULTS_FILE"

echo "Scenario,Safety Level,AI Enabled,Files Before,Files After,Lines Before,Lines After,Reduction %,Status,Duration (s)" > "$METRICS_FILE"

# Function to count SQL files
count_files() {
  local dir=$1
  find "$dir" -name "*.sql" 2>/dev/null | wc -l | tr -d ' '
}

# Function to count total lines
count_lines() {
  local dir=$1
  local total=$(find "$dir" -name "*.sql" -exec wc -l {} + 2>/dev/null | tail -1 | awk '{print $1}')
  if [ -z "$total" ] || [ "$total" = "0" ]; then
    echo "0"
  else
    echo "$total"
  fi
}

# Function to run squash for a scenario
run_squash() {
  local scenario=$1
  local config_file="$TEST_CONFIGS_DIR/${scenario}.json"
  local output_dir="$SQUASHED_BASE_DIR/${scenario}"
  local log_file="$LOG_DIR/${scenario}-squash.log"

  echo -e "${BLUE}[$(date +%H:%M:%S)]${NC} Running squash for ${YELLOW}${scenario}${NC}..."

  # Clean output directory
  rm -rf "$output_dir"
  mkdir -p "$output_dir"

  # Record start time
  local start_time=$(date +%s)

  # Run squash command (note: glob pattern must not be quoted)
  if ./pgsquash squash $MIGRATIONS_DIR/*.sql \
    --config "$config_file" \
    --output "$output_dir" \
    > "$log_file" 2>&1; then

    local end_time=$(date +%s)
    local duration=$((end_time - start_time))

    echo -e "${GREEN}✓${NC} Squash completed in ${duration}s"
    return 0
  else
    local end_time=$(date +%s)
    local duration=$((end_time - start_time))

    echo -e "${RED}✗${NC} Squash failed after ${duration}s (check $log_file)"
    return 1
  fi
}

# Function to collect metrics
collect_metrics() {
  local scenario=$1
  local safety_level=$(echo "$scenario" | cut -d'-' -f1)
  local ai_status=$(echo "$scenario" | grep -q "ai$" && echo "enabled" || echo "disabled")
  local output_dir="$SQUASHED_BASE_DIR/${scenario}"
  local log_file="$LOG_DIR/${scenario}-squash.log"
  local status="SUCCESS"
  local duration=0

  # Extract duration from execution
  if [ -f "$log_file" ]; then
    duration=$(grep -oP 'completed in \K[0-9]+' "$log_file" 2>/dev/null || echo "0")
  fi

  # Count files and lines
  local files_before=$(count_files "$MIGRATIONS_DIR")
  local files_after=$(count_files "$output_dir")
  local lines_before=$(count_lines "$MIGRATIONS_DIR")
  local lines_after=$(count_lines "$output_dir")

  # Calculate reduction percentage
  local reduction=0
  if [ "$lines_before" -gt 0 ]; then
    reduction=$(awk "BEGIN {printf \"%.1f\", (($lines_before - $lines_after) / $lines_before) * 100}")
  fi

  # Check for failures
  if [ ! -d "$output_dir" ] || [ "$files_after" -eq 0 ]; then
    status="FAILED"
  fi

  # Write to CSV
  echo "$scenario,$safety_level,$ai_status,$files_before,$files_after,$lines_before,$lines_after,$reduction%,$status,$duration" >> "$METRICS_FILE"

  # Write to text results
  {
    echo "Scenario: $scenario"
    echo "  Safety Level: $safety_level"
    echo "  AI: $ai_status"
    echo "  Files: $files_before → $files_after ($(($files_before - $files_after)) removed)"
    echo "  Lines: $lines_before → $lines_after ($reduction% reduction)"
    echo "  Status: $status"
    echo "  Duration: ${duration}s"
    echo ""
  } >> "$RESULTS_FILE"
}

# Main execution
main() {
  echo -e "${BLUE}=========================================${NC}"
  echo -e "${BLUE}E2E Validation Test Runner${NC}"
  echo -e "${BLUE}=========================================${NC}"
  echo ""

  # Check if pgsquash binary exists
  if [ ! -f "$PROJECT_ROOT/pgsquash" ]; then
    echo -e "${RED}Error: pgsquash binary not found. Building...${NC}"
    cd "$PROJECT_ROOT" && go build -o pgsquash cmd/pgsquash/main.go
  fi

  # Verify migrations exist
  local migration_count=$(count_files "$MIGRATIONS_DIR")
  echo -e "Found ${GREEN}${migration_count}${NC} migration files"
  echo ""

  # Execute all scenarios
  local success_count=0
  local total_count=${#SCENARIOS[@]}

  for scenario in "${SCENARIOS[@]}"; do
    if run_squash "$scenario"; then
      collect_metrics "$scenario"
      ((success_count++))
    else
      collect_metrics "$scenario"
    fi
    echo ""
  done

  # Final summary
  echo -e "${BLUE}=========================================${NC}"
  echo -e "${BLUE}Execution Summary${NC}"
  echo -e "${BLUE}=========================================${NC}"
  echo -e "Total scenarios: ${total_count}"
  echo -e "Successful: ${GREEN}${success_count}${NC}"
  echo -e "Failed: ${RED}$((total_count - success_count))${NC}"
  echo ""
  echo -e "Results saved to:"
  echo -e "  - ${YELLOW}${RESULTS_FILE}${NC}"
  echo -e "  - ${YELLOW}${METRICS_FILE}${NC}"
  echo -e "  - Logs: ${YELLOW}${LOG_DIR}/${NC}"
  echo ""

  # Write completion to results file
  {
    echo "==========================================="
    echo "Completed: $(date)"
    echo "Success rate: $success_count/$total_count"
    echo "==========================================="
  } >> "$RESULTS_FILE"

  # Next steps
  echo -e "${YELLOW}Next steps:${NC}"
  echo "  1. Review execution logs in: $LOG_DIR"
  echo "  2. Run validation: ./scripts/validate.sh --mode full --migrations squashed/<scenario>"
  echo "  3. Check metrics: cat $METRICS_FILE"
}

# Run main function
main "$@"
