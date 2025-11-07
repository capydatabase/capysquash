#!/bin/bash

echo "=== Checking profiles CREATE TABLE columns ==="
echo ""

# Extract profiles CREATE TABLE from squashed output (all on one line)
PROFILES_CREATE=$(grep -o 'CREATE TABLE IF NOT EXISTS profiles ([^;]*)' "case-study-outputs/myroomie-standard-no-ai-FIXED-V5/000_baseline.sql")

if [ -z "$PROFILES_CREATE" ]; then
    echo "ERROR: Could not find profiles CREATE TABLE"
    exit 1
fi

echo "Found profiles CREATE TABLE"
echo "Total length: ${#PROFILES_CREATE} characters"
echo ""

# Check for specific missing columns
MISSING_COLS=("property_management_scope" "managed_properties_count" "portfolio_size_limit" "auth_provider")

echo "Checking for missing columns:"
for col in "${MISSING_COLS[@]}"; do
    if echo "$PROFILES_CREATE" | grep -q "$col"; then
        echo "  ✓ $col: FOUND"
    else
        echo "  ✗ $col: MISSING"
    fi
done

echo ""
echo "Checking original migrations:"
for i in 01 02 03; do
    FILE="./case studies/myroomie/migrations/${i}_*.sql"
    HAS_PROP=$(grep -l "property_management_scope" $FILE 2>/dev/null || echo "")
    if [ -n "$HAS_PROP" ]; then
        echo "  Migration $i: HAS property_management_scope"
    else
        echo "  Migration $i: NO property_management_scope"
    fi
done
