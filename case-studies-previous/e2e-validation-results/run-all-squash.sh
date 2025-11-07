#!/bin/bash
# E2E Validation Runner for pgsquash-engine
# Executes squash operations across all test configurations

set -e

PROJECT_ROOT="/Users/dominikospritis/DevFolder/pg-squash/pgsquash-engine"
cd "$PROJECT_ROOT"

# Ensure binary is built
if [ ! -f "./pgsquash" ]; then
    echo "Building pgsquash binary..."
    go build -o pgsquash cmd/pgsquash/main.go
fi

# Test configurations
CONFIGS=(
    "paranoid-no-ai"
    "paranoid-ai"
    "conservative-no-ai"
    "conservative-ai"
    "standard-no-ai"
    "standard-ai"
    "aggressive-no-ai"
    "aggressive-ai"
)

RESULTS_DIR="e2e-validation-results"
mkdir -p "$RESULTS_DIR"

echo "=========================================="
echo "E2E VALIDATION: SQUASH OPERATIONS"
echo "=========================================="
echo "Total configurations: ${#CONFIGS[@]}"
echo "Migration source: migrations/*.sql"
echo "Started: $(date)"
echo ""

for config in "${CONFIGS[@]}"; do
    echo "------------------------------------------"
    echo "Configuration: $config"
    echo "------------------------------------------"

    CONFIG_FILE="test-configs/${config}.json"
    OUTPUT_DIR="squashed/${config}"
    LOG_FILE="$RESULTS_DIR/${config}-squash.log"
    METRICS_FILE="$RESULTS_DIR/${config}-metrics.txt"

    # Clean output directory
    rm -rf "$OUTPUT_DIR"
    mkdir -p "$OUTPUT_DIR"

    echo "Config file: $CONFIG_FILE"
    echo "Output directory: $OUTPUT_DIR"
    echo "Log file: $LOG_FILE"
    echo ""

    # Execute squash with timing
    START_TIME=$(date +%s)

    if ./pgsquash squash \
        --config "$CONFIG_FILE" \
        migrations/*.sql \
        > "$LOG_FILE" 2>&1; then

        END_TIME=$(date +%s)
        DURATION=$((END_TIME - START_TIME))

        echo "✓ Squash completed successfully"
        echo "Duration: ${DURATION}s"

        # Capture metrics
        {
            echo "============================================"
            echo "SQUASH METRICS: $config"
            echo "============================================"
            echo "Timestamp: $(date)"
            echo "Duration: ${DURATION}s"
            echo ""
            echo "OUTPUT FILES:"
            ls -lh "$OUTPUT_DIR/" 2>/dev/null || echo "No files generated"
            echo ""
            echo "FILE SIZES:"
            du -sh "$OUTPUT_DIR" 2>/dev/null || echo "0B"
            echo ""
            echo "STATEMENT COUNTS:"
            for sql_file in "$OUTPUT_DIR"/*.sql; do
                if [ -f "$sql_file" ]; then
                    count=$(grep -c ";" "$sql_file" 2>/dev/null || echo "0")
                    echo "  $(basename "$sql_file"): $count statements"
                fi
            done
            echo ""
            echo "LOG SUMMARY:"
            echo "---"
            grep -E "(Total|Reduction|Warning|Error|statements|migrations)" "$LOG_FILE" | head -20
            echo "---"
        } > "$METRICS_FILE"

        echo "Metrics saved to: $METRICS_FILE"

    else
        END_TIME=$(date +%s)
        DURATION=$((END_TIME - START_TIME))

        echo "✗ Squash FAILED"
        echo "Duration: ${DURATION}s"
        echo "Check log file for details: $LOG_FILE"

        # Save error metrics
        {
            echo "============================================"
            echo "SQUASH FAILED: $config"
            echo "============================================"
            echo "Timestamp: $(date)"
            echo "Duration: ${DURATION}s"
            echo ""
            echo "ERROR LOG:"
            echo "---"
            cat "$LOG_FILE"
            echo "---"
        } > "$METRICS_FILE"

        # Continue with other configs even if one fails
    fi

    echo ""
done

echo "=========================================="
echo "ALL SQUASH OPERATIONS COMPLETED"
echo "=========================================="
echo "Completed: $(date)"
echo "Results directory: $RESULTS_DIR"
echo ""
echo "Summary of outputs:"
for config in "${CONFIGS[@]}"; do
    OUTPUT_DIR="squashed/${config}"
    if [ -d "$OUTPUT_DIR" ] && [ "$(ls -A "$OUTPUT_DIR" 2>/dev/null)" ]; then
        file_count=$(ls -1 "$OUTPUT_DIR"/*.sql 2>/dev/null | wc -l)
        total_size=$(du -sh "$OUTPUT_DIR" | cut -f1)
        echo "  $config: $file_count files, $total_size total"
    else
        echo "  $config: FAILED or no output"
    fi
done
echo ""
echo "Next step: Run validation with Docker containers"
echo "./scripts/validate.sh --mode full --migrations squashed/<config>/"
