#!/bin/bash
# Drizzle reset script - freezes current state, applies squashed migrations, and resets tracking
# This script helps integrate pgsquash with Drizzle ORM's migration system

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${GREEN}=== Drizzle Reset Script ===${NC}\n"

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

# Backup current migrations directory
MIGRATIONS_DIR="drizzle"
if [ ! -d "$MIGRATIONS_DIR" ]; then
    MIGRATIONS_DIR="migrations"
fi

if [ ! -d "$MIGRATIONS_DIR" ]; then
    echo -e "${RED}Error: Could not find migrations directory (drizzle/ or migrations/)${NC}"
    exit 1
fi

BACKUP_DIR="${MIGRATIONS_DIR}_backup_$(date +%Y%m%d_%H%M%S)"
echo -e "${YELLOW}Creating backup of migrations directory...${NC}"
cp -r "$MIGRATIONS_DIR" "$BACKUP_DIR"
echo -e "${GREEN}✓ Backup created: $BACKUP_DIR${NC}\n"

# Read squashmap
echo -e "${YELLOW}Reading squashmap...${NC}"
BASELINE_FILE=$(jq -r '.outputs[] | select(contains("baseline") or contains("000"))' .squashmap.json | head -n 1)
DATA_FILE=$(jq -r '.outputs[] | select(contains("data") or contains("010"))' .squashmap.json | head -n 1)

if [ -z "$BASELINE_FILE" ]; then
    echo -e "${RED}Error: Could not find baseline file in squashmap${NC}"
    exit 1
fi

# Step 1: Drop and recreate the database schema (DESTRUCTIVE - use with caution)
echo -e "${BLUE}Step 1: Resetting database schema${NC}"
echo -e "${YELLOW}⚠️  This will drop all tables and recreate from squashed migrations${NC}"
read -p "Continue? [yes/NO]: " -r
echo

if [[ ! $REPLY =~ ^[Yy][Ee][Ss]$ ]]; then
    echo -e "${YELLOW}Operation cancelled${NC}"
    exit 1
fi

# Drop the __drizzle_migrations table to reset tracking
echo "Dropping Drizzle migration tracking table..."
psql "$DATABASE_URL" -c "DROP TABLE IF EXISTS __drizzle_migrations CASCADE;" 2>/dev/null || true

# Step 2: Apply squashed baseline
echo -e "\n${BLUE}Step 2: Applying squashed baseline${NC}"
echo "Applying: $BASELINE_FILE"
psql "$DATABASE_URL" -f "$BASELINE_FILE"

if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ Baseline schema applied${NC}"
else
    echo -e "${RED}✗ Failed to apply baseline schema${NC}"
    exit 1
fi

# Step 3: Apply data operations if they exist
if [ -n "$DATA_FILE" ] && [ -f "$DATA_FILE" ]; then
    echo -e "\n${BLUE}Step 3: Applying data operations${NC}"
    echo "Applying: $DATA_FILE"
    psql "$DATABASE_URL" -f "$DATA_FILE"
    
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✓ Data operations applied${NC}"
    else
        echo -e "${YELLOW}⚠ Warning: Data operations failed${NC}"
    fi
fi

# Step 4: Clean up old migrations and move squashed files
echo -e "\n${BLUE}Step 4: Reorganizing migration files${NC}"

# Remove old migration files (they're backed up)
echo "Removing old migration files..."
rm -rf "${MIGRATIONS_DIR}"/*

# Copy squashed migrations to the migrations directory
echo "Moving squashed migrations to $MIGRATIONS_DIR..."
cp "$BASELINE_FILE" "${MIGRATIONS_DIR}/"
if [ -n "$DATA_FILE" ] && [ -f "$DATA_FILE" ]; then
    cp "$DATA_FILE" "${MIGRATIONS_DIR}/"
fi

# Copy squashmap for reference
cp .squashmap.json "${MIGRATIONS_DIR}/"

echo -e "${GREEN}✓ Migration directory reorganized${NC}"

# Step 5: Generate new Drizzle metadata
echo -e "\n${BLUE}Step 5: Regenerating Drizzle metadata${NC}"
echo "Running: drizzle-kit generate"

if command -v drizzle-kit &> /dev/null; then
    drizzle-kit generate
    echo -e "${GREEN}✓ Drizzle metadata regenerated${NC}"
else
    echo -e "${YELLOW}⚠ drizzle-kit not found. Run 'npx drizzle-kit generate' manually${NC}"
fi

# Summary
echo ""
echo -e "${GREEN}=== Summary ===${NC}"
echo "✓ Database schema reset"
echo "✓ Squashed migrations applied"
echo "✓ Old migrations backed up to: $BACKUP_DIR"
echo "✓ Migration directory reorganized"
echo ""
echo -e "${BLUE}Next steps:${NC}"
echo "1. Test your application to ensure everything works"
echo "2. Commit the new migration structure"
echo "3. Have team members:"
echo "   - Pull the changes"
echo "   - Drop their local database"
echo "   - Run: drizzle-kit push (or your migration command)"
echo "4. If everything works, you can delete: $BACKUP_DIR"
echo ""
echo -e "${GREEN}✓ Drizzle reset complete!${NC}"
