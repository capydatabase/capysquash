#!/bin/bash
# Prisma baseline script - applies squashed migrations and marks old ones as applied
# This script helps integrate pgsquash with Prisma's migration system

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== Prisma Baseline Script ===${NC}\n"

# Check if .squashmap.json exists
if [ ! -f ".squashmap.json" ]; then
    echo -e "${RED}Error: .squashmap.json not found${NC}"
    echo "Please run pgsquash first to generate the squashmap file"
    exit 1
fi

# Check if jq is installed
if ! command -v jq &> /dev/null; then
    echo -e "${RED}Error: jq is required but not installed${NC}"
    echo "Install with: brew install jq (macOS) or apt-get install jq (Linux)"
    exit 1
fi

# Check if DATABASE_URL is set
if [ -z "$DATABASE_URL" ]; then
    echo -e "${RED}Error: DATABASE_URL environment variable is not set${NC}"
    echo "Set it with: export DATABASE_URL='postgresql://user:password@localhost:5432/dbname'"
    exit 1
fi

# Read squashmap to get list of original migrations
echo -e "${YELLOW}Reading squashmap...${NC}"
MIGRATIONS=$(jq -r '.inputs[]' .squashmap.json | sed 's/.*\///' | sed 's/\.sql$//')
MIGRATION_COUNT=$(echo "$MIGRATIONS" | wc -l | tr -d ' ')

echo -e "${GREEN}Found $MIGRATION_COUNT migrations to mark as applied${NC}\n"

# Apply the squashed baseline schema
echo -e "${YELLOW}Applying squashed baseline schema...${NC}"
BASELINE_FILE=$(jq -r '.outputs[] | select(contains("baseline") or contains("000"))' .squashmap.json | head -n 1)

if [ -z "$BASELINE_FILE" ]; then
    echo -e "${RED}Error: Could not find baseline file in squashmap${NC}"
    exit 1
fi

if [ ! -f "$BASELINE_FILE" ]; then
    echo -e "${RED}Error: Baseline file not found: $BASELINE_FILE${NC}"
    exit 1
fi

echo "Applying: $BASELINE_FILE"
psql "$DATABASE_URL" -f "$BASELINE_FILE"

if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ Baseline schema applied successfully${NC}\n"
else
    echo -e "${RED}✗ Failed to apply baseline schema${NC}"
    exit 1
fi

# Apply data operations file if it exists
DATA_FILE=$(jq -r '.outputs[] | select(contains("data") or contains("010"))' .squashmap.json | head -n 1)

if [ -n "$DATA_FILE" ] && [ -f "$DATA_FILE" ]; then
    echo -e "${YELLOW}Applying data operations...${NC}"
    echo "Applying: $DATA_FILE"
    psql "$DATABASE_URL" -f "$DATA_FILE"
    
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ Data operations applied successfully${NC}\n"
    else
        echo -e "${YELLOW}⚠ Warning: Data operations failed (may be expected if data already exists)${NC}\n"
    fi
fi

# Mark old migrations as applied in Prisma
echo -e "${YELLOW}Marking old migrations as applied in Prisma...${NC}"

MARKED_COUNT=0
FAILED_COUNT=0

for migration in $MIGRATIONS; do
    echo "Marking as applied: $migration"
    
    # Use npx to run prisma migrate resolve
    if npx prisma migrate resolve --applied "$migration" 2>/dev/null; then
        MARKED_COUNT=$((MARKED_COUNT + 1))
        echo -e "${GREEN}  ✓ Marked${NC}"
    else
        FAILED_COUNT=$((FAILED_COUNT + 1))
        echo -e "${YELLOW}  ⚠ Already marked or not found${NC}"
    fi
done

echo ""
echo -e "${GREEN}=== Summary ===${NC}"
echo "Total migrations:     $MIGRATION_COUNT"
echo "Successfully marked:  $MARKED_COUNT"
echo "Skipped/Failed:       $FAILED_COUNT"
echo ""

if [ $MARKED_COUNT -gt 0 ]; then
    echo -e "${GREEN}✓ Prisma baseline complete!${NC}"
    echo ""
    echo "Next steps:"
    echo "1. Commit the squashed migrations"
    echo "2. Have team members pull and run: npx prisma migrate deploy"
    echo "3. Delete old migration files from the migrations directory"
else
    echo -e "${YELLOW}⚠ No migrations were marked. Check if they were already applied.${NC}"
fi
